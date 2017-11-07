package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
)

type SensuClientEc2 struct {
	Tags map[string]string `json:"tags"`
}

type SensuClient struct {
	Name          string         `json:"name"`
	Address       string         `json:"address"`
	Subscriptions []string       `json:"subscriptions"`
	Ec2           SensuClientEc2 `json:"ec2,omitempty"`
}

// Usage: instancesByRegion -api <url> -state <value> [-state value...] [-region region...] [-tag key=value...]
func main() {
	apis, states, regions, tags := parseArguments()

	if len(apis) == 0 {
		fmt.Fprintf(os.Stderr, "Error: %v\n", usage())
		os.Exit(1)
	}

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
					err := discoverInstance(apis, instance)
					if err != nil {
						fmt.Println("Error:", err)
					}
				}
			}
		}
	}
}

func manageSensuProxyClient(apis []string, client SensuClient) error {
	random := rand.New(rand.NewSource(time.Now().Unix()))
	randomInt := random.Intn(len(apis))

	api, err := url.Parse(apis[randomInt])

	if err != nil {
		return err
	}

	clientsUrl := "http://" + api.Host + "/clients"

	httpClient := &http.Client{}

	req, err := http.NewRequest("GET", clientsUrl+"/"+client.Name, nil)
	if api.User != nil {
		user := api.User.Username()
		pass, _ := api.User.Password()
		req.SetBasicAuth(user, pass)
	}

	getResp, err := httpClient.Do(req)

	if err != nil {
		return err
	}
	defer getResp.Body.Close()

	switch getResp.StatusCode {
	case 404:
		data, _ := json.Marshal(client)

		req, err := http.NewRequest("POST", clientsUrl, bytes.NewBuffer(data))
		req.Header.Set("Content-Type", "application/json")
		if api.User != nil {
			user := api.User.Username()
			pass, _ := api.User.Password()
			req.SetBasicAuth(user, pass)
		}

		postResp, err := httpClient.Do(req)

		if err != nil {
			return err
		}
		defer postResp.Body.Close()
	case 401:
		return errors.New("Sensu API authentication failure")
	}

	return nil
}

func discoverInstance(apis []string, instance *ec2.Instance) error {
	client := SensuClient{
		Name:          *instance.InstanceId,
		Address:       *instance.PublicDnsName,
		Subscriptions: []string{},
		Ec2: SensuClientEc2{
			Tags: make(map[string]string),
		},
	}
	for _, tag := range instance.Tags {
		client.Ec2.Tags[*tag.Key] = *tag.Value
	}

	err := manageSensuProxyClient(apis, client)
	return err
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

func parseArguments() (apis []string, states []string, regions []string, tags []string) {
	var apiArgs, stateArgs, regionArgs, tagArgs flagArgs

	flag.Var(&apiArgs, "api", "api url")
	flag.Var(&stateArgs, "state", "state list")
	flag.Var(&regionArgs, "region", "region list")
	flag.Var(&tagArgs, "tag", "tag key=value list")
	flag.Parse()

	if flag.NFlag() != 0 {
		apis = append([]string{}, apiArgs.Args()...)
		states = append([]string{}, stateArgs.Args()...)
		regions = append([]string{}, regionArgs.Args()...)
		tags = append([]string{}, tagArgs.Args()...)
	}

	return apis, states, regions, tags
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
