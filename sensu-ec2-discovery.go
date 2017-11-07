package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"

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

// Usage: instancesByRegion -state <value> [-state value...] [-region region...] [-tag key=value...]
func main() {
	states, regions, tags := parseArguments()

	if len(states) == 0 {
		fmt.Fprintf(os.Stderr, "Error: %v\n", usage())
		os.Exit(1)
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
			fmt.Println("Error", err)
		} else {
			for _, reservation := range result.Reservations {
				for _, instance := range reservation.Instances {
					err := discoverInstance(instance)
					if err != nil {
						fmt.Println("Error", err)
					}
				}
			}
		}
	}
}

func manageSensuProxyClient(client SensuClient) error {
	baseUrl := "http://127.0.0.1:4567/clients"

	httpClient := &http.Client{}

	req, err := http.NewRequest("GET", baseUrl+"/"+client.Name, nil)
	req.SetBasicAuth("foo", "bar")

	getResp, err := httpClient.Do(req)

	if err != nil {
		return err
	}
	defer getResp.Body.Close()

	if getResp.StatusCode == 404 {
		data, _ := json.Marshal(client)

		req, err := http.NewRequest("POST", baseUrl, bytes.NewBuffer(data))
		req.Header.Set("Content-Type", "application/json")
		req.SetBasicAuth("foo", "bar")

		postResp, err := httpClient.Do(req)

		if err != nil {
			return err
		}
		defer postResp.Body.Close()
	}

	return nil
}

func discoverInstance(instance *ec2.Instance) error {
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

	err := manageSensuProxyClient(client)
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

func parseArguments() (states []string, regions []string, tags []string) {
	var stateArgs, regionArgs, tagArgs flagArgs

	flag.Var(&stateArgs, "state", "state list")
	flag.Var(&regionArgs, "region", "region list")
	flag.Var(&tagArgs, "tag", "tag list")
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

Missing mandatory flag 'state'.

To discover running instances in every region:
	./sensu-ec2-discover -state running

To discover running and pending instances in specific regions:
	./sensu-ec2-discover -state running -state pending -region us-west-1 -region us-west-2

To discover running instances with a specific tag key/value:
	./sensu-ec2-discover -state running -tag environment=production
`
}
