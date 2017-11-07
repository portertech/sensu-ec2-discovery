package main

import (
	"encoding/json"
	"flag"
	"fmt"
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

	filters := []*ec2.Filter{
		&ec2.Filter{
			Name:   aws.String("instance-state-name"),
			Values: aws.StringSlice(states),
		},
	}

	if len(regions) == 0 {
		var err error
		regions, err = fetchRegion()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}

	if len(tags) != 0 {
		for _, tag := range tags {
			tagPair := strings.Split(tag, "=")
			filter := &ec2.Filter{
				Name:   aws.String(strings.Join([]string{"tag", tagPair[0]}, ":")),
				Values: []*string{aws.String(tagPair[1])},
			}
			filters = append(filters, filter)
		}
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
					fmt.Printf("%v\n", instance)
					sensuClient := SensuClient{
						Name:          *instance.InstanceId,
						Address:       *instance.PublicDnsName,
						Subscriptions: []string{},
						Ec2: SensuClientEc2{
							Tags: make(map[string]string),
						},
					}
					for _, tag := range instance.Tags {
						sensuClient.Ec2.Tags[*tag.Key] = *tag.Value
					}
					output, _ := json.Marshal(sensuClient)
					fmt.Printf("%s\n", output)
				}
			}
		}
	}
}

func fetchRegion() ([]string, error) {
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
