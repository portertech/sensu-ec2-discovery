package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
)

// Usage:
// sensu-ec2-discovery <region> <tag_key>=<tag_value>
func main() {
	sess := session.Must(session.NewSession())

	awsRegion := os.Args[1]

	tag := os.Args[2]
	tag_pair := strings.Split(tag, "=")
	tag_key := tag_pair[0]
	tag_value := tag_pair[1]

	svc := ec2.New(sess, &aws.Config{Region: aws.String(awsRegion)})

	fmt.Printf("listing instances with tag %v in: %v\n", tag, awsRegion)

	params := &ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			{
				Name: aws.String(strings.Join([]string{"tag", tag_key}, ":")),
				Values: []*string{
					aws.String(tag_value),
				},
			},
		},
	}
	resp, err := svc.DescribeInstances(params)
	if err != nil {
		fmt.Println("there was an error listing instances in", awsRegion, err.Error())
		log.Fatal(err.Error())
	}
	fmt.Printf("%+v\n", *resp)
}
