package mgmt

import (
	"fmt"

	"github.com/vishvananda/netlink"

	"github.com/harvester/harvester-network-controller/pkg/network/iface"
)

type CiliumNetwork struct {
	vtep *netlink.Vxlan
	nic  iface.IFace
}

func NewCiliumNetwork(device string) (*CiliumNetwork, error) {
	link, err := netlink.LinkByName(device)
	if err != nil {
		return nil, fmt.Errorf("failed to find link %s, error: %w", device, err)
	}

	// VTEP = Virtual Tunnel Endpoint
	vtep, ok := link.(*netlink.Vxlan)
	if !ok {
		return nil, fmt.Errorf("got data of type %T but wanted *netlink.Vxlan", link)
	}

	nic, err := iface.GetLinkByIndex(vtep.VtepDevIndex)
	if err != nil {
		return nil, fmt.Errorf("failed to find link with index %d, error: %w", vtep.VtepDevIndex, err)
	}

	return &CiliumNetwork{
		vtep: vtep,
		nic:  nic,
	}, nil
}

func (f *CiliumNetwork) Type() string {
	return "cilium"
}

func (f *CiliumNetwork) Setup(nic string) error {
	return nil
}

func (f *CiliumNetwork) Teardown() error {
	return nil
}

func (f *CiliumNetwork) NIC() iface.IFace {
	return f.nic
}
