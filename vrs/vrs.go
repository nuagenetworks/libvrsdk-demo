package vrs

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/docker/distribution/uuid"
	"github.com/nuagenetworks/libvrsdk/api"
	"github.com/nuagenetworks/libvrsdk/api/entity"
	"github.com/nuagenetworks/libvrsdk/api/port"
	"math/rand"
	"os"
	"os/exec"
	"time"
)

type VMType int

const (
	VM        VMType = 0
	Container VMType = 1
)

type NuageInfo struct {
	UnixSocketFile string
	BridgeName     string
	VSDAddress     string
	Subnet         string
	Org            string
	Domain         string
	Zone           string
	User           string
	CspUser        string
	CspPassword    string
}

type VMData struct {
	Eth            string
	UUID           string
	PortName       string
	Mac            string
	IP             string
	Netmask        string
	InterfaceVSDId string
}

var vrsConnection api.VRSConnection
var NuageData NuageInfo

func execCmd(c string) {
	fmt.Println(c)
	cmd := exec.Command("bash", "-c", c)
	_, err := cmd.Output()
	if err != nil {
		e := fmt.Sprintf("Error: %s", err)
		fmt.Println(e)
		os.Exit(1)
	}
}

func generateMAC() string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	a := r.Intn(100)
	b := r.Intn(100)
	c := r.Intn(100)

	mac := fmt.Sprintf("fe:00:fd:%d:%d:%d", a, b, c)
	return mac
}

func getPortInfo(port string) *api.PortIPv4Info {

	portInfoUpdateChan := make(chan *api.PortIPv4Info)
	vrsConnection.RegisterForPortUpdates(port, portInfoUpdateChan)
	portInfo := &api.PortIPv4Info{}
	select {
	case portInfo = <-portInfoUpdateChan:
		return portInfo
	}
}

func getVSDAuth() string {
	auth := NuageData.CspUser + ":" + NuageData.CspPassword
	auth = base64.StdEncoding.EncodeToString([]byte(auth))
	cmdstr := fmt.Sprintf("curl -k -H 'X-Nuage-Organization: csp' -H 'Authorization: XREST %s' https://"+NuageData.VSDAddress+"/nuage/api/v4_0/me", auth)
	cmd := exec.Command("bash", "-c", cmdstr)
	o, _ := cmd.Output()

	var data []interface{}
	json.Unmarshal(o, &data)
	auth = NuageData.CspUser + ":" + (data[0].(map[string]interface{}))["APIKey"].(string)
	auth = base64.StdEncoding.EncodeToString([]byte(auth))
	fmt.Println(auth)
	return auth
}

func vsdSplitActivation(vsdid string, vmname string, uuid string, portname string, mac string) string {

	auth := getVSDAuth()

	cmdstr := fmt.Sprintf("curl -k -X POST -H 'Content-Type: application/json' -H 'X-Nuage-Organization: csp' -H 'Authorization: XREST %s' -d '{\"name\": \"%s\",\"UUID\": \"%s\",\"interfaces\":[{\"name\":\"%s\",\"VPortID\":\"%s\",\"MAC\":\"%s\"}]}' https://"+NuageData.VSDAddress+"/nuage/api/v4_0/vms", auth, vmname, uuid, portname, vsdid, mac)
	cmd := exec.Command("bash", "-c", cmdstr)
	o, _ := cmd.Output()
	var data []interface{}
	json.Unmarshal(o, &data)
	id := ((data[0].(map[string]interface{}))["interfaces"].([]interface{}))[0].(map[string]interface{})["ID"].(string)
	return id
}

func Connect() {
	var err error
	if vrsConnection, err = api.NewUnixSocketConnection(NuageData.UnixSocketFile); err != nil {
		fmt.Println("Unable to connect to the VRS")
	}
}

func getMetaData(splitActivation bool) (map[port.MetadataKey]string, map[entity.MetadataKey]string) {
	portMetadata := make(map[port.MetadataKey]string)

	if !splitActivation {
		portMetadata[port.MetadataKeyDomain] = NuageData.Domain
		portMetadata[port.MetadataKeyNetwork] = NuageData.Subnet
		portMetadata[port.MetadataKeyZone] = NuageData.Zone
		portMetadata[port.MetadataKeyNetworkType] = "ipv4"
	} else {
		portMetadata[port.MetadataKeyDomain] = ""
		portMetadata[port.MetadataKeyNetwork] = ""
		portMetadata[port.MetadataKeyZone] = ""
		portMetadata[port.MetadataKeyNetworkType] = ""
	}

	vmMetadata := make(map[entity.MetadataKey]string)
	if !splitActivation {
		vmMetadata[entity.MetadataKeyUser] = NuageData.User
		vmMetadata[entity.MetadataKeyEnterprise] = NuageData.Org
	} else {
		vmMetadata[entity.MetadataKeyUser] = ""
		vmMetadata[entity.MetadataKeyEnterprise] = ""
		vmMetadata[entity.MetadataKeyExtension] = "true"
	}

	return portMetadata, vmMetadata
}

func CreateVMNet(name string, eth string, vsdid string) VMData {
	mac := generateMAC()
	ret := createVM(name, eth, vsdid, VM, mac)
	return ret
}

func CreateContainerNet(name string, localVeth string) VMData {
	mac := generateMAC()
	ret := createVM(name, localVeth, "", Container, mac)
	return ret
}

func createVM(name string, eth string, vsdid string, vmType VMType, mac string) VMData {
	var PortName string
	var VMName string
	var UUID string
	var err error
	var interfacevsdid string

	PortName = eth
	UUID = uuid.Generate().String()
	VMName = eth

	Connect()

	cmdstr := fmt.Sprintf("/usr/bin/ovs-vsctl --no-wait --if-exists del-port alubr0 %s -- add-port alubr0 %s -- set interface %s 'external-ids={vm-uuid=%s,vm-name=%s}'", eth, eth, eth, UUID, VMName)
	execCmd(cmdstr)

	portMetadata, vmMetadata := getMetaData(vsdid != "")

	portAttributes := port.Attributes{
		MAC:    mac,
		Bridge: NuageData.BridgeName,
	}

	entityInfo := api.EntityInfo{
		UUID:     UUID,
		Name:     VMName,
		Ports:    []string{PortName},
		Metadata: vmMetadata,
	}

	if vmType == VM {
		portAttributes.Platform = entity.KVM
		entityInfo.Type = entity.VM
		entityInfo.Domain = entity.KVM
	} else if vmType == Container {
		portAttributes.Platform = entity.Docker
		entityInfo.Type = entity.Container
		entityInfo.Domain = entity.Docker
	}

	if vsdid != "" {
		interfacevsdid = vsdSplitActivation(vsdid, VMName, UUID, PortName, mac)
	}

	err = vrsConnection.CreatePort(PortName, portAttributes, portMetadata)
	if err != nil {
		fmt.Println("Unable to add port the VRS")
	}

	err = vrsConnection.CreateEntity(entityInfo)
	if err != nil {
		fmt.Println("Unable to add entity to VRS")
	}

	// Notify VRS that VM has completed booted
	err = vrsConnection.PostEntityEvent(UUID, entity.EventCategoryStarted, entity.EventStartedBooted)
	if err != nil {
		fmt.Println("Problem sending VRS notification")
	}

	portInfo := getPortInfo(PortName)

	return VMData{
		Eth:            eth,
		UUID:           UUID,
		PortName:       PortName,
		Mac:            portInfo.MAC,
		IP:             portInfo.IPAddr,
		Netmask:        portInfo.Mask,
		InterfaceVSDId: interfacevsdid,
	}
}

func DestroyNet(info VMData) {
	var cmdstr string

	if info.InterfaceVSDId != "" {
		xrest := getVSDAuth()
		execCmd("curl -k -X DELETE -H 'X-Nuage-Organization: csp' -H 'Authorization: XREST " + xrest + "' https://192.168.99.3:8443/nuage/api/v4_0/vminterfaces/" + info.InterfaceVSDId)
	}

	// Delete Entity
	vrsConnection.DestroyEntity(info.UUID)

	// Delete port
	vrsConnection.DestroyPort(info.PortName)

	// Remove from bridge
	cmdstr = fmt.Sprintf("/usr/bin/ovs-vsctl --no-wait del-port %s %s", NuageData.BridgeName, info.Eth)
	execCmd(cmdstr)
	vrsConnection.Disconnect()
}
