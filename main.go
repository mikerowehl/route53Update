package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/rdegges/go-ipify"
)

// Looks up the HostedZone info for a group of records on route53. I've been
// using this to update the apex record for the domain I use, so it checks to
// see if the name of the hosted zone exactly matches the domain.
func GetHostedZone(client *route53.Client, domain string) (*types.HostedZone, error) {
	req := &route53.ListHostedZonesByNameInput{
		DNSName: &domain,
	}

	res, err := client.ListHostedZonesByName(context.TODO(), req)
	if err != nil {
		return nil, fmt.Errorf("Failed to get hosted zones: %v", err)
	}

	for _, zone := range res.HostedZones {
		if *zone.Name == domain {
			return &zone, nil
		}
	}
	return nil, fmt.Errorf("Can't match domain %s to zone", domain)
}

// Return the ip address of the A rec for the overall domain. I use this with
// a very simple setup, so I just return the first value for the resource
// record set that matches the exact domain and has type A rec.
func GetARecIp(client *route53.Client, zone string, domain string) (string, error) {
	req := &route53.ListResourceRecordSetsInput{
		HostedZoneId: aws.String(zone),
	}

	recs, err := client.ListResourceRecordSets(context.TODO(), req)
	if err != nil {
		return "", err
	}

	for _, rec := range recs.ResourceRecordSets {
		if *rec.Name == domain && rec.Type == types.RRTypeA {
			return *rec.ResourceRecords[0].Value, nil
		}
	}
	return "", fmt.Errorf("Could not find A rec for top level name")
}

// Changes the top level A rec for the domain passed in to point to the ip
// addr provided. Also, very simple and static, assume just a single record
// for the current address and that's it.
func UpdateIp(client *route53.Client, zone string, domain string, ip string) (*route53.ChangeResourceRecordSetsOutput, error) {
	change := types.Change{
		Action: types.ChangeActionUpsert,
		ResourceRecordSet: &types.ResourceRecordSet{
			Name: aws.String(domain),
			Type: types.RRTypeA,
			ResourceRecords: []types.ResourceRecord{
				{
					Value: aws.String(ip),
				},
			},
			TTL: aws.Int64(300),
		},
	}
	params := &route53.ChangeResourceRecordSetsInput{
		ChangeBatch: &types.ChangeBatch{
			Changes: []types.Change{change},
		},
		HostedZoneId: aws.String(zone),
	}

	res, err := client.ChangeResourceRecordSets(context.TODO(), params)
	return res, err
}

func main() {
	// All the calls want full domain format, but that's not what I
	// normally give as a domain name, so tack on the period at the end
	domain := os.Args[1] + "."

	// Get our public IP by using the ipify server to tell us what it
	// tooks like our IP address is
	ip, err := ipify.GetIp()
	if err != nil {
		log.Fatalf("Failed getting current ip: %v", err)
	}
	fmt.Printf("Current ip address: %s\n", ip)

	// Load up the default AWS config, assuming it can read and write to
	// route53 for the domain we want to use
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		log.Fatalf("Unable to load AWS config: %v", err)
	}
	client := route53.NewFromConfig(cfg)

	// We need the zone id and not just the domain
	zone, err := GetHostedZone(client, domain)
	if err != nil {
		log.Fatalf("Failed to find zone: %v", err)
	}
	fmt.Printf("Found zone: %s\n", *zone.Id)

	// Look up the IP address current in route53
	configuredIp, err := GetARecIp(client, *zone.Id, domain)
	if err != nil {
		log.Fatalf("Error trying to check configured ip: %v", err)
	}
	fmt.Printf("Address in route53 is %s\n", configuredIp)

	// If our public IP and what's in route53 match we're done
	if ip == configuredIp {
		fmt.Printf("Address already up to date, done\n")
		return
	}

	// If the addresses don't match, update route53
	change, err := UpdateIp(client, *zone.Id, domain, ip)
	if err != nil {
		log.Fatalf("Error trying to update record: %v", err)
	}

	fmt.Printf("Updated. Change: %s\n", *change.ChangeInfo.Id)
}
