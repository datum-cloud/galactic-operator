package cniconfig

import (
	"fmt"
	"net"

	galacticv1alpha "github.com/datum-cloud/galactic-operator/api/v1alpha"

	"github.com/kenshaw/baseconv"
)

// types inlined from CNI and Galactic CNI packages to simplify cross dependencies
type NetConfList struct {
	CNIVersion string      `json:"cniVersion"`
	Plugins    interface{} `json:"plugins"`
}

type PluginConfGalactic struct {
	Type          string        `json:"type"`
	VPC           string        `json:"vpc"`
	VPCAttachment string        `json:"vpcattachment"`
	MTU           int           `json:"mtu,omitempty"`
	Terminations  []Termination `json:"terminations,omitempty"`
}

type Termination struct {
	Network string `json:"network"`
	Via     string `json:"via,omitempty"`
}

type PluginConfHostDevice struct {
	Type   string `json:"type"`
	Device string `json:"device"`
	IPAM   IPAM   `json:"ipam,omitempty"`
}

type IPAM struct {
	Type      string    `json:"type"`
	Routes    []Route   `json:"routes,omitempty"`
	Addresses []Address `json:"addresses,omitempty"`
}

type Route struct {
	Dst string `json:"dst"`
	GW  string `json:"gw,omitempty"`
}

type Address struct {
	Address string `json:"address"`
}

func CNIConfigForVPCAttachment(vpc galacticv1alpha.VPC, vpcAttachment galacticv1alpha.VPCAttachment) (NetConfList, error) {
	terminations := make([]Termination, 0, 10)
	addresses := make([]Address, 0, 10)
	routes := make([]Route, 0, 10)

	netAddresses := make([]net.IP, 0, 10) // to check if a route is local

	for _, address := range vpcAttachment.Spec.Interface.Addresses {
		netAddress, network, err := net.ParseCIDR(address)
		if err != nil {
			return NetConfList{}, err
		}
		netAddresses = append(netAddresses, netAddress)
		addresses = append(addresses, Address{Address: address})
		terminations = append(terminations, Termination{Network: network.String()})
	}

	for _, route := range vpcAttachment.Spec.Routes {
		_, network, err := net.ParseCIDR(route.Destination)
		if err != nil {
			return NetConfList{}, err
		}

		if route.Via != "" {
			via := net.ParseIP(route.Via)
			if via == nil {
				return NetConfList{}, fmt.Errorf("failed to parse route via %q", route.Via)
			}

			local := false
			for _, netAddress := range netAddresses {
				if via.Equal(netAddress) {
					local = true
					break
				}
			}
			if local { // local routes are terminations
				terminations = append(terminations, Termination{Network: network.String(), Via: via.String()})
			} else {
				routes = append(routes, Route{Dst: network.String(), GW: via.String()})
			}
		}
	}

	vpcIdentifierBase62, err := baseconv.Convert(vpc.Status.Identifier, baseconv.DigitsHex, baseconv.Digits62)
	if err != nil {
		return NetConfList{}, err
	}
	vpcAttachmentIdentifierBase62, err := baseconv.Convert(vpcAttachment.Status.Identifier, baseconv.DigitsHex, baseconv.Digits62)
	if err != nil {
		return NetConfList{}, err
	}

	// TODO Change to use VPC & VPCAttachment identifiers once CNI is adjusted
	return NetConfList{
		CNIVersion: "0.4.0",
		Plugins: []interface{}{
			PluginConfGalactic{
				Type:          "galactic-cni",
				VPC:           vpcIdentifierBase62,
				VPCAttachment: vpcAttachmentIdentifierBase62,
				MTU:           1300,
				Terminations:  terminations,
			},
			PluginConfHostDevice{
				Type:   "host-device",
				Device: fmt.Sprintf("G%s%sG", vpcIdentifierBase62, vpcAttachmentIdentifierBase62),
				IPAM: IPAM{
					Type:      "static",
					Addresses: addresses,
					Routes:    routes,
				},
			},
		},
	}, nil
}
