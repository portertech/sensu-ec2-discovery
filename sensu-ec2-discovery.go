package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	corev2 "github.com/sensu/sensu-go/api/core/v2"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
)

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
	entity.EntityClass = "proxy"
	entity.Labels = make(map[string]string)
	for _, tag := range instance.Tags {
		entity.Labels[*tag.Key] = *tag.Value
	}

	fmt.Printf("%s\n", entity.Name)
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
