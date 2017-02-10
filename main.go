package main

import (
	"bufio"
	"github.com/nuagenetworks/libvrsdk-demo/vrs"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"time"
)

func execCmd(c string) {
	fmt.Println(c)
	cmd := exec.Command("bash", "-c", c)
	_, err := cmd.Output()
	if err != nil {
		e := fmt.Sprintf("Error: %s", err)
		fmt.Println(e)
	}
}

func startContainer(name string) vrs.VMData {
	// Create veth pair
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	portid := fmt.Sprint(r.Intn(100))
	localVeth := "vethl" + portid
	remoteVeth := "vethr" + portid
	execCmd("/sbin/ip link add " + localVeth + " type veth peer name " + remoteVeth)
	execCmd("/sbin/ip link set " + localVeth + " up")
	containerInfo := vrs.CreateContainerNet("test", localVeth)

	//[]string{localVeth,remoteVeth,UUID, PortName, portInfo.MAC, portInfo.IPAddr, portInfo.Mask}
	execCmd("/sbin/ip link set " + remoteVeth + " address " + containerInfo.Mac)
	execCmd("/sbin/ip netns add " + name)
	execCmd("/sbin/ip link set dev " + remoteVeth + " netns " + name)
	execCmd("/sbin/ip netns exec " + name + " ip addr add " + containerInfo.IP + "/" + containerInfo.Netmask + " dev " + remoteVeth)
	execCmd("/sbin/ip netns exec " + name + " ip link set dev " + remoteVeth + " up")
	return containerInfo
}

func stopContainer(info vrs.VMData, name string) {
	vrs.DestroyNet(info)
	execCmd("/sbin/ip netns del " + name)

}

func startVM(name string, vsdid string) vrs.VMData {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	v := r.Intn(100)
	vnc := fmt.Sprint(v)
	eth := "tap" + vnc

	execCmd("/sbin/ip tuntap add " + eth + " mode tap")
	execCmd("/sbin/ifconfig " + eth + " 0.0.0.0 promisc up")
	containerInfo := vrs.CreateVMNet("test", eth, vsdid)
	fmt.Println(containerInfo)

	vncPortString := fmt.Sprint((5900 + v))
	//[]string{eth,UUID, PortName, portInfo.MAC, portInfo.IPAddr, portInfo.Mask}
	cmdStr := "/usr/libexec/qemu-kvm -cpu host -enable-kvm -drive file=" + name + ",if=virtio -m 2048 -net tap,ifname=" + eth + ",script=no -net nic,model=virtio,macaddr=" + containerInfo.Mac + " -vnc 0.0.0.0:" + vnc
	fmt.Println(cmdStr)
	cmd := exec.Command("bash", "-c", cmdStr)
	cmd.Start()
	fmt.Println("\nVNC on port " + vncPortString + " Press enter to terminate")
	bufio.NewReader(os.Stdin).ReadBytes('\n')
	cmd.Process.Kill()
	cmd.Wait()

	return containerInfo
}

func stopVM(info vrs.VMData) {
	vrs.DestroyNet(info)
	execCmd("/sbin/ip tuntap del " + info.Eth + " mode tap")
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage:")
		fmt.Println(os.Args[0] + " -c")
		fmt.Println("\tThis will create a pair of veth interfaces, create a new network namespace and move one of the veth")
		fmt.Println("\tthat namespace. The other veth will be added in alubr0. You will then be dropped in a bash shell ")
		fmt.Println("\tusing that network namespace")
		fmt.Println(os.Args[0] + " -vm <path_to_disk_file>")
		fmt.Println("\tLaunch qemu-kvm with the provided image file. Will create a tap interface and add it in alubr0")
		fmt.Println(os.Args[0] + " -split <path_to_disk_file> <vsdid_port>")
		fmt.Println("\tLaunch qemu-kvm with the provided image file. Will create a tap interface and add it in alubr0")
		fmt.Println("\tThis will make use of split-activation using the provided ID of the port in VSD")
		os.Exit(1)
	}

	if os.Args[1] == "-c" {
		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		ns := "dumais" + fmt.Sprint(r.Intn(1000))
		info := startContainer(ns)
		cmd := exec.Command("bash", "-c", "/sbin/ip netns exec "+ns+" bash")
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Run()
		stopContainer(info, ns)
	} else if os.Args[1] == "-vm" {
		info := startVM(os.Args[2], "")
		stopVM(info)
	} else if os.Args[1] == "-split" {
		info := startVM(os.Args[2], os.Args[3])
		stopVM(info)
	} else if os.Args[1] == "-clean" {
		eth := os.Args[2]
		UUID := os.Args[3]
		PortName := os.Args[2]
		vrs.Connect()
		vrs.DestroyNet(vrs.VMData{Eth: eth, UUID: UUID, PortName: PortName})
		execCmd("/sbin/ip tuntap del " + eth + " mode tap")
	}
}
