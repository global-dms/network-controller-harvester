package nad

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	hn "github.com/harvester/harvester/pkg/api/network"
	ctlcniv1 "github.com/harvester/harvester/pkg/generated/controllers/k8s.cni.cncf.io/v1"
	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"github.com/vishvananda/netlink"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog"

	networkv1 "github.com/harvester/harvester-network-controller/pkg/apis/network.harvesterhci.io/v1beta1"
	"github.com/harvester/harvester-network-controller/pkg/config"
	ctlnetworkv1 "github.com/harvester/harvester-network-controller/pkg/generated/controllers/network.harvesterhci.io/v1beta1"
	"github.com/harvester/harvester-network-controller/pkg/network"
	"github.com/harvester/harvester-network-controller/pkg/network/mgmt"
	"github.com/harvester/harvester-network-controller/pkg/network/vlan"
	"github.com/harvester/harvester-network-controller/pkg/utils"
)

// Harvester network nad watches network-attachment-definition CR, retrieve network configuration and make it effective.
// For example, the controller get VLAN ID from nad and add it to physical NIC attached with bridge.
const (
	ControllerName = "harvester-network-nad-controller"

	vlanLabelKey = networkv1.GroupName + "/vlan-id"
)

type Handler struct {
	nodeNetworkCache ctlnetworkv1.NodeNetworkCache
	nadCache         ctlcniv1.NetworkAttachmentDefinitionCache
	mgmtNetwork      network.Network
}

func Register(ctx context.Context, management *config.Management) error {
	nad := management.CniFactory.K8s().V1().NetworkAttachmentDefinition()
	nns := management.HarvesterNetworkFactory.Network().V1beta1().NodeNetwork()

	handler := &Handler{
		nodeNetworkCache: nns.Cache(),
		nadCache:         nad.Cache(),
	}

	switch management.Options.MgmtNetworkType {
	case "flannel", "canal":
		mgmtNetwork, err := mgmt.NewFlannelNetwork(management.Options.MgmtNetworkDevice)
		if err != nil {
			return err
		}
		handler.mgmtNetwork = mgmtNetwork
	}

	nad.OnChange(ctx, ControllerName, handler.OnChange)
	nad.OnRemove(ctx, ControllerName, handler.OnRemove)

	return nil
}

func (h Handler) OnChange(key string, nad *nadv1.NetworkAttachmentDefinition) (*nadv1.NetworkAttachmentDefinition, error) {
	if nad == nil || nad.DeletionTimestamp != nil {
		return nil, nil
	}

	if nad.Spec.Config == "" {
		return nad, nil
	}

	klog.Infof("nad configuration %s has been changed: %s", nad.Name, nad.Spec.Config)
	netconf := &hn.NetConf{}
	if err := json.Unmarshal([]byte(nad.Spec.Config), netconf); err != nil {
		return nil, err
	}

	// TODO delete previous vlan id when update nad

	v, err := vlan.GetVlan(h.mgmtNetwork)
	if err != nil && !errors.As(err, &netlink.LinkNotFoundError{}) && !errors.As(err, &vlan.SlaveNotFoundError{}) {
		return nil, err
	} else if err != nil {
		klog.Infof("ignore link not found error, details: %+v", err)
		return nad, nil
	}

	layer3NetworkConf := &utils.Layer3NetworkConf{}
	if nad.Annotations != nil && nad.Annotations[utils.KeyNetworkConf] != "" {
		if layer3NetworkConf, err = utils.NewLayer3NetworkConf(nad.Annotations[utils.KeyNetworkConf]); err != nil {
			return nil, err
		}
	}

	if err := v.AddLocalArea(netconf.Vlan, layer3NetworkConf.CIDR); err != nil {
		return nil, err
	}

	return nad, nil
}

func (h Handler) OnRemove(key string, nad *nadv1.NetworkAttachmentDefinition) (*nadv1.NetworkAttachmentDefinition, error) {
	if nad == nil {
		return nil, nil
	}

	if nad.Spec.Config == "" {
		return nad, nil
	}

	klog.Infof("nad configuration %s has been deleted.", nad.Name)

	netconf := &hn.NetConf{}
	if err := json.Unmarshal([]byte(nad.Spec.Config), netconf); err != nil {
		return nil, err
	}

	// there may be multiple nad CR with the same vlan id
	labelSet := labels.Set(map[string]string{
		vlanLabelKey: strconv.Itoa(netconf.Vlan),
	})
	nads, err := h.nadCache.List("", labelSet.AsSelector())
	if err != nil {
		return nil, err
	}
	if len(nads) > 1 {
		return nil, nil
	}

	v, err := vlan.GetVlan(h.mgmtNetwork)
	if err != nil && !errors.As(err, &netlink.LinkNotFoundError{}) && !errors.As(err, &vlan.SlaveNotFoundError{}) {
		return nil, err
	} else if err != nil {
		klog.Infof("ignore link not found error, details: %+v", err)
		return nad, nil
	}

	layer3NetworkConf := &utils.Layer3NetworkConf{}
	if nad.Annotations != nil && nad.Annotations[utils.KeyNetworkConf] != "" {
		layer3NetworkConf, err = utils.NewLayer3NetworkConf(nad.Annotations[utils.KeyNetworkConf])
		if err != nil {
			return nil, err
		}
	}

	if err := v.RemoveLocalArea(netconf.Vlan, layer3NetworkConf.CIDR); err != nil {
		return nil, err
	}

	return nad, nil
}
