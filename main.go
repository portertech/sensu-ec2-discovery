package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"

	corev2 "github.com/sensu/sensu-go/api/core/v2"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
)

type Authentication struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Expiration   int64  `json:"expires_at"`
}

var (
	sensuApiUrl      string = getenv("SENSU_API_URL", "http://127.0.0.1:8080")
	sensuApiCertFile string = getenv("SENSU_API_CERT_FILE", "")
	sensuApiUser     string = getenv("SENSU_API_USER", "admin")
	sensuApiPass     string = getenv("SENSU_API_PASS", "P@ssw0rd!")
	sensuApiToken    string
)

func getenv(key string, fallback string) string {
	value := os.Getenv(key)
	if len(value) == 0 {
		return fallback
	}
	return value
}

func authenticate(httpClient *http.Client) string {
	var authentication Authentication
	req, err := http.NewRequest(
		"GET",
		fmt.Sprintf("%s/auth", sensuApiUrl),
		nil,
	)
	if err != nil {
		log.Fatal("ERROR: ", err)
	}
	req.SetBasicAuth(sensuApiUser, sensuApiPass)
	resp, err := httpClient.Do(req)
	if err != nil {
		log.Fatalf("ERROR: %s", err)
	} else if resp.StatusCode == 401 {
		log.Fatalf("ERROR: %v %s (please check your access credentials)", resp.StatusCode, http.StatusText(resp.StatusCode))
	} else if resp.StatusCode >= 300 {
		log.Fatalf("ERROR: %v %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	defer resp.Body.Close()
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatal("ERROR: ", err)
	}
	err = json.NewDecoder(bytes.NewReader(b)).Decode(&authentication)
	if err != nil {
		log.Fatal("ERROR: ", err)
	}
	return authentication.AccessToken
}

func LoadCACerts(path string) (*x509.CertPool, error) {
	rootCAs, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("Error loading system cert pool: %s", err)
	}
	if rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}
	if path != "" {
		certs, err := ioutil.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("Error reading CA file (%s): %s", path, err)
		} else {
			rootCAs.AppendCertsFromPEM(certs)
		}
	}
	return rootCAs, nil
}

func initHttpClient() *http.Client {
	certs, err := LoadCACerts(sensuApiCertFile)
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

// Usage: instancesByRegion -api <url> -state <value> [-state value...] [-region region...] [-tag key=value...]
func main() {
	states, regions, tags := parseArguments()

	if len(states) == 0 {
		states = []string{"running"}
	}

	if len(regions) == 0 {
		var err error
		regions, err = fetchRegions()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}

	filters, err := createFilters(states, tags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	for _, region := range regions {
		sess := session.Must(session.NewSession(&aws.Config{
			Region: aws.String(region),
		}))

		ec2Svc := ec2.New(sess)

		params := &ec2.DescribeInstancesInput{Filters: filters}
		result, err := ec2Svc.DescribeInstances(params)

		if err != nil {
			fmt.Println("Error:", err)
		} else {
			for _, reservation := range result.Reservations {
				for _, instance := range reservation.Instances {
					discoverInstance(instance)
				}
			}
		}
	}
}

func discoverInstance(instance *ec2.Instance) {
	var entity corev2.Entity
	entity.Name = *instance.InstanceId
	entity.Namespace = "default"
	entity.EntityClass = "proxy"
	entity.Labels = make(map[string]string)
	for _, tag := range instance.Tags {
		entity.Labels[*tag.Key] = *tag.Value
	}

	fmt.Printf("%s\n", entity.Name)

	postBody, err := json.Marshal(entity)
	if err != nil {
		log.Fatal("ERROR: ", err)
	}
	body := bytes.NewReader(postBody)
	req, err := http.NewRequest(
		"POST",
		fmt.Sprintf("%s/api/core/v2/namespaces/%s/entities",
			sensuApiUrl,
			entity.Namespace,
		),
		body,
	)
	if err != nil {
		log.Fatal("ERROR: ", err)
	}
	var httpClient *http.Client = initHttpClient()
	sensuApiToken = authenticate(httpClient)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", sensuApiToken))
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		log.Fatalf("ERROR: %s\n", err)
	} else if resp.StatusCode == 404 {
		log.Fatalf("ERROR: %v %s (%s)\n", resp.StatusCode, http.StatusText(resp.StatusCode), req.URL)
	} else if resp.StatusCode == 409 {
		log.Printf("INFO: %v %s; entity \"%s\" already exists\n", resp.StatusCode, http.StatusText(resp.StatusCode), entity.Name)
	} else if resp.StatusCode >= 300 {
		log.Fatalf("ERROR: %v %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	defer resp.Body.Close()
	b, err := ioutil.ReadAll(resp.Body)
	fmt.Println(string(b))

	return
}

func createFilters(states []string, tags []string) ([]*ec2.Filter, error) {
	filters := []*ec2.Filter{
		&ec2.Filter{
			Name:   aws.String("instance-state-name"),
			Values: aws.StringSlice(states),
		},
	}

	for _, tag := range tags {
		tagPair := strings.Split(tag, "=")
		filter := &ec2.Filter{
			Name:   aws.String(strings.Join([]string{"tag", tagPair[0]}, ":")),
			Values: []*string{aws.String(tagPair[1])},
		}
		filters = append(filters, filter)
	}

	return filters, nil
}

func fetchRegions() ([]string, error) {
	awsSession := session.Must(session.NewSession(&aws.Config{}))

	svc := ec2.New(awsSession)
	awsRegions, err := svc.DescribeRegions(&ec2.DescribeRegionsInput{})
	if err != nil {
		return nil, err
	}

	regions := make([]string, 0, len(awsRegions.Regions))
	for _, region := range awsRegions.Regions {
		regions = append(regions, *region.RegionName)
	}

	return regions, nil
}

type flagArgs []string

func (a flagArgs) String() string {
	return strings.Join(a.Args(), ",")
}

func (a *flagArgs) Set(value string) error {
	*a = append(*a, value)
	return nil
}

func (a flagArgs) Args() []string {
	return []string(a)
}

func parseArguments() (states []string, regions []string, tags []string) {
	var stateArgs, regionArgs, tagArgs flagArgs

	flag.Var(&stateArgs, "state", "state list")
	flag.Var(&regionArgs, "region", "region list")
	flag.Var(&tagArgs, "tag", "tag key=value list")
	flag.Parse()

	if flag.NFlag() != 0 {
		states = append([]string{}, stateArgs.Args()...)
		regions = append([]string{}, regionArgs.Args()...)
		tags = append([]string{}, tagArgs.Args()...)
	}

	return states, regions, tags
}

func usage() string {
	return `

Missing mandatory flag 'api'.

To discover running instances in every region:
	./sensu-ec2-discover -api http://user:password@127.0.0.1:4567

To discover running and pending instances in specific regions:
	./sensu-ec2-discover -api http://user:password@127.0.0.1:4567 -state running -state pending -region us-west-1 -region us-west-2

To discover running instances with a specific tag key/value:
	./sensu-ec2-discover -api http://user:password@127.0.0.1:4567 -tag environment=production

To balance the Sensu API request load accross several Sensu APIs:
	./sensu-ec2-discover -api http://user:password@host1:4567 -api http://user:password@host2:4567
`
}
