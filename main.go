package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"

	corev2 "github.com/sensu/sensu-go/api/core/v2"
	"github.com/calebhailey/sensu-plugins-go-library/sensu"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
)

type CheckConfig struct {
	sensu.PluginConfig
	ec2InstanceStates          string
	ec2InstanceRegions         string
	ec2InstanceTags            string
	ec2Filters                 []*ec2.Filter
	sensuNamespace             string
	sensuApiUrl                string
	sensuAccessToken           string
	sensuTrustedCaFile         string
	sensuInsecureSkipTlsVerify string
}

var (
	config = CheckConfig{
		PluginConfig: sensu.PluginConfig{
			Name:     "sensu-ec2-discovery",
			Short:    "Auto-discover EC2 instances and update your Sensu Go registry.",
			Keyspace: "sensu.io/plugins/ec2-discovery",
		},
	}

	ec2DiscoveryConfigOptions = []*sensu.PluginConfigOption{
		{
			Path:      "ec2-instance-states",
			Env:       "EC2_INSTANCE_STATES",
			Argument:  "ec2-instance-states",
			Shorthand: "s",
			Usage:     "The AWS EC2 instance states to discover. Can also be set via the $EC2_INSTANCE_STATES environment variable.",
			Value:     &config.ec2InstanceStates,
			Default:   "pending,running,rebooting",
		},
		{
			Path:      "ec2-instance-regions",
			Env:       "EC2_INSTANCE_REGIONS",
			Argument:  "ec2-instance-regions",
			Shorthand: "r",
			Usage:     "The AWS EC2 region(s) to discover. Can also be set via the $EC2_INSTANCE_REGIONS environment variable. OPTIONAL.",
			Value:     &config.ec2InstanceRegions,
			Default:   "",
		},
		{
			Path:      "ec2-instance-tags",
			Env:       "EC2_INSTANCE_TAGS",
			Argument:  "ec2-instance-tags",
			Shorthand: "t",
			Usage:     "The AWS Cloudwatch metric dimension. Can also be set via the $EC2_INSTANCE_TAGS environment variable. OPTIONAL.",
			Value:     &config.ec2InstanceTags,
			Default:   "",
		},
		{
			Path:      "sensu-namespace",
			Env:       "SENSU_NAMESPACE",
			Argument:  "sensu-namespace",
			Shorthand: "",
			Usage:     "The Sensu Go Namespace to register entities in. Can also be set via the $SENSU_NAMESPACE environment variable.",
			Value:     &config.sensuNamespace,
			Default:   "default",
		},
		{
			Path:      "sensu-api-url",
			Env:       "SENSU_API_URL",
			Argument:  "sensu-api-url",
			Shorthand: "",
			Usage:     "The Sensu Go API URL. Can also be set via the $SENSU_API_URL environment variable.",
			Value:     &config.sensuApiUrl,
			Default:   "https://127.0.0.1:8080",
		},
		{
			Path:      "sensu-access-token",
			Env:       "SENSU_ACCESS_TOKEN",
			Argument:  "sensu-access-token",
			Shorthand: "",
			Usage:     "The Sensu Go API access key. Can also be set via the $SENSU_ACCESS_TOKEN environment variable. REQUIRED.",
			Value:     &config.sensuAccessToken,
			Default:   "",
		},
		{
			Path:      "sensu-trusted-ca-file",
			Env:       "SENSU_TRUSTED_CA_FILE",
			Argument:  "sensu-trusted-ca-file",
			Shorthand: "",
			Usage:     "The Sensu Go API URL. Can also be set via the $SENSU_TRUSTED_CA_FILE environment variable. OPTIONAL.",
			Value:     &config.sensuTrustedCaFile,
			Default:   "",
		},
		{
			Path:      "sensu-insecure-tls-skip-verify",
			Env:       "SENSU_INSECURE_SKIP_TLS_VERIFY",
			Argument:  "sensu-insecure-tls-skip-verify",
			Shorthand: "",
			Usage:     "The Sensu Go API URL. Can also be set via the $SENSU_INSECURE_SKIP_TLS_VERIFY environment variable.",
			Value:     &config.sensuInsecureSkipTlsVerify,
			Default:   "false",
		},
	}
)

func main() {
	check := sensu.InitCheck(
		&config.PluginConfig,
		ec2DiscoveryConfigOptions,
		validateArgs,
		discoverInstances,
	)
	check.Execute()
}

func validateArgs(event *corev2.Event) error {
	if config.sensuAccessToken == "" {
		log.Fatalf("ERROR: no Sensu API access token provided. Exiting.")
		return fmt.Errorf("No Sensu API access token provided. Exiting.")
	}

	err := createFilters()
	if err != nil {
		log.Fatalf("ERROR: %s\n", err)
		return err
	}

	return nil
}

func createFilters() error {
	var states []string
	var tags []string

	if len(config.ec2InstanceStates) > 0 {
		states = strings.Split(config.ec2InstanceStates, ",")
		config.ec2Filters = append(config.ec2Filters, &ec2.Filter{
			Name:   aws.String("instance-state-name"),
			Values: aws.StringSlice(states),
		})
	}

	if len(config.ec2InstanceTags) > 0 {
		tags = strings.Split(config.ec2InstanceTags, ",")
		for _, tag := range tags {
			tagPair := strings.Split(tag, "=")
			filter := &ec2.Filter{
				Name:   aws.String(strings.Join([]string{"tag", tagPair[0]}, ":")),
				Values: []*string{aws.String(tagPair[1])},
			}
			config.ec2Filters = append(config.ec2Filters, filter)
		}
	}

	return nil
}

func LoadCACerts(path string) (*x509.CertPool, error) {
	rootCAs, err := x509.SystemCertPool()
	if err != nil {
		log.Fatalf("ERROR: failed to load system cert pool: %s", err)
		return nil, err
	}
	if rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}
	if path != "" {
		certs, err := ioutil.ReadFile(path)
		if err != nil {
			log.Fatalf("ERROR: failed to read CA file (%s): %s", path, err)
			return nil, err
		} else {
			rootCAs.AppendCertsFromPEM(certs)
		}
	}
	return rootCAs, nil
}

func initHttpClient() *http.Client {
	certs, err := LoadCACerts(config.sensuTrustedCaFile)
	if err != nil {
		log.Fatalf("ERROR: %s\n", err)
	}
	tlsConfig := &tls.Config{
		RootCAs: certs,
	}
	tr := &http.Transport{
		TLSClientConfig: tlsConfig,
	}
	client := &http.Client{
		Transport: tr,
	}
	return client
}

func registerInstance(instance *ec2.Instance) {
	var entity corev2.Entity
	entity.Name = *instance.InstanceId
	entity.Namespace = config.sensuNamespace
	entity.EntityClass = "proxy"
	entity.Labels = make(map[string]string)
	for _, tag := range instance.Tags {
		entity.Labels[*tag.Key] = *tag.Value
	}

	// fmt.Printf("%s\n", entity.Name)

	postBody, err := json.Marshal(entity)
	if err != nil {
		log.Fatal("ERROR: ", err)
	}
	body := bytes.NewReader(postBody)
	req, err := http.NewRequest(
		"POST",
		fmt.Sprintf("%s/api/core/v2/namespaces/%s/entities",
			config.sensuApiUrl,
			entity.Namespace,
		),
		body,
	)
	if err != nil {
		log.Fatal("ERROR: ", err)
	}
	var httpClient *http.Client = initHttpClient()
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", config.sensuAccessToken))
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		log.Fatalf("ERROR: %s\n", err)
	} else if resp.StatusCode == 404 {
		log.Fatalf("ERROR: %v %s (%s)\n", resp.StatusCode, http.StatusText(resp.StatusCode), req.URL)
	} else if resp.StatusCode == 409 {
		log.Printf("INFO: entity \"%s\" already exists (%v: %s)\n", entity.Name, resp.StatusCode, http.StatusText(resp.StatusCode))
	} else if resp.StatusCode >= 300 {
		log.Fatalf("ERROR: %v %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	} else if resp.StatusCode == 201 {
		log.Printf("INFO: registered entity for EC2 instance \"%s\"", entity.Name)
	} else {
		defer resp.Body.Close()
		b, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Fatalf("ERROR: %s\n", err)
		} else {
			fmt.Printf("%s\n", string(b))
		}
	}

	return
}

// Usage: instancesByRegion -api <url> -state <value> [-state value...] [-region region...] [-tag key=value...]
func discoverInstances(event *corev2.Event) error {
	for _, region := range strings.Split(config.ec2InstanceRegions, ",") {
		aws_session := session.Must(session.NewSession(&aws.Config{
			Region: aws.String(region),
		}))

		svc := ec2.New(aws_session)

		params := &ec2.DescribeInstancesInput{Filters: config.ec2Filters}
		result, err := svc.DescribeInstances(params)
		if err != nil {
			log.Fatalf("ERROR: %s\n", err)
			return err
		} else {
			for _, reservation := range result.Reservations {
				for _, instance := range reservation.Instances {
					registerInstance(instance)
				}
			}
		}
	}
	return nil
}
