package docker

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"strconv"
	"strings"
	"bytes"
	"errors"
	"gopkg.in/yaml.v2"
	"os"

	"github.com/azukaar/cosmos-server/src/utils"
	"github.com/docker/docker/api/types"

	conttype "github.com/docker/docker/api/types/container"
	strslice "github.com/docker/docker/api/types/strslice"
)

var ExportError = "" 

// FormatByteSize converts a raw byte count (as reported by the docker daemon's
// Resources/HostConfig fields) into a docker-compose-style byte-size string
// such as "64mb" or "1gb". This keeps byte-value fields (shm_size, mem_limit,
// mem_reservation) consistent with the {amount}{unit} format docker-compose
// expects, so round-tripped exports behave the same as the originals.
func FormatByteSize(bytes int64) string {
	if bytes <= 0 {
		return ""
	}
	const (
		KiB = 1024
		MiB = 1024 * KiB
		GiB = 1024 * MiB
	)
	switch {
	case bytes % GiB == 0:
		return strconv.FormatInt(bytes / GiB, 10) + "gb"
	case bytes % MiB == 0:
		return strconv.FormatInt(bytes / MiB, 10) + "mb"
	case bytes % KiB == 0:
		return strconv.FormatInt(bytes / KiB, 10) + "kb"
	default:
		return strconv.FormatInt(bytes, 10) + "b"
	}
} 

func ExportContainer(containerID string) (ContainerCreateRequestContainer, error)  {
		// Fetch detailed info of each container
		detailedInfo, err := DockerClient.ContainerInspect(DockerContext, containerID)
		if err != nil {
			ExportError = "Export Docker - Cannot inspect container" + containerID + " - " + err.Error()
			return ContainerCreateRequestContainer{}, errors.New(ExportError)
		}

		// Map the detailedInfo to your ContainerCreateRequestContainer struct
		// Here's a simplified example, you'd need to handle all the fields
		service := ContainerCreateRequestContainer{
			Name:         strings.TrimPrefix(detailedInfo.Name, "/"),
			Image:        detailedInfo.Config.Image,
			Environment:  detailedInfo.Config.Env,
			Labels:       detailedInfo.Config.Labels,
			Command:      strslice.StrSlice(detailedInfo.Config.Cmd),
			Entrypoint:   strslice.StrSlice(detailedInfo.Config.Entrypoint),
			WorkingDir:   detailedInfo.Config.WorkingDir,
			User:         detailedInfo.Config.User,
			Tty:          detailedInfo.Config.Tty,
			StdinOpen:    detailedInfo.Config.OpenStdin,
			Hostname:     func () string { 
				if string(detailedInfo.HostConfig.NetworkMode) == "bridge" || string(detailedInfo.HostConfig.NetworkMode) == "default" {
					return detailedInfo.Config.Hostname
				}
				return ""
			}(),
			Domainname:   detailedInfo.Config.Domainname,
			MacAddress:   detailedInfo.NetworkSettings.MacAddress,
			// Normalize container/service refs to stable container:<name>: the
			// inspect may report a container ID that goes stale on recreate.
			NetworkMode:  ContainerRefToName(string(detailedInfo.HostConfig.NetworkMode)),
			StopSignal:   detailedInfo.Config.StopSignal,
			HealthCheck:  ContainerCreateRequestContainerHealthcheck {
			},
			DNS:              detailedInfo.HostConfig.DNS,
			DNSSearch:        detailedInfo.HostConfig.DNSSearch,
			Runtime:		  detailedInfo.HostConfig.Runtime,
			ExtraHosts:       detailedInfo.HostConfig.ExtraHosts,
			SecurityOpt:      detailedInfo.HostConfig.SecurityOpt,
			StorageOpt:       detailedInfo.HostConfig.StorageOpt,
			Sysctls:          detailedInfo.HostConfig.Sysctls,
			Isolation:        string(detailedInfo.HostConfig.Isolation),
			ShmSize:          ByteSize(FormatByteSize(detailedInfo.HostConfig.ShmSize)),
			CapAdd:           detailedInfo.HostConfig.CapAdd,
			CapDrop:          detailedInfo.HostConfig.CapDrop,
			Privileged:       detailedInfo.HostConfig.Privileged,

			// Resource constraints
			MemLimit: func() ByteSize {
				if detailedInfo.HostConfig.Resources.Memory > 0 {
					return ByteSize(FormatByteSize(detailedInfo.HostConfig.Resources.Memory))
				}
				return ""
			}(),
			MemReservation: func() ByteSize {
				if detailedInfo.HostConfig.Resources.MemoryReservation > 0 {
					return ByteSize(FormatByteSize(detailedInfo.HostConfig.Resources.MemoryReservation))
				}
				return ""
			}(),
			CPUs:       float64(detailedInfo.HostConfig.Resources.NanoCPUs) / 1e9,
			CPUShares:  detailedInfo.HostConfig.Resources.CPUShares,
			Cpuset:     detailedInfo.HostConfig.Resources.CpusetCpus,

			// Additional resource constraints (docker-compose parity)
			MemSwapLimit: func() ByteSize {
				ms := detailedInfo.HostConfig.Resources.MemorySwap
				if ms == -1 {
					return "-1"
				}
				if ms > 0 {
					return ByteSize(FormatByteSize(ms))
				}
				return ""
			}(),
			CPUPeriod:          detailedInfo.HostConfig.Resources.CPUPeriod,
			CPUQuota:           detailedInfo.HostConfig.Resources.CPUQuota,
			CPURealtimePeriod:  detailedInfo.HostConfig.Resources.CPURealtimePeriod,
			CPURealtimeRuntime: detailedInfo.HostConfig.Resources.CPURealtimeRuntime,
			MemSwappiness: func() int {
				if detailedInfo.HostConfig.Resources.MemorySwappiness != nil {
					return int(*detailedInfo.HostConfig.Resources.MemorySwappiness)
				}
				return 0
			}(),
			OomKillDisable: func() bool {
				return detailedInfo.HostConfig.Resources.OomKillDisable != nil && *detailedInfo.HostConfig.Resources.OomKillDisable
			}(),
			PidsLimit: func() int64 {
				if detailedInfo.HostConfig.Resources.PidsLimit != nil {
					return *detailedInfo.HostConfig.Resources.PidsLimit
				}
				return 0
			}(),
			CpusetMems: detailedInfo.HostConfig.Resources.CpusetMems,
			Ulimits: func() []string {
				uls := []string{}
				for _, u := range detailedInfo.HostConfig.Resources.Ulimits {
					uls = append(uls, u.String())
				}
				return uls
			}(),
			BlkioConfig: func() *ContainerCreateRequestServiceBlkioConfig {
				rc := detailedInfo.HostConfig.Resources
				if rc.BlkioWeight == 0 && len(rc.BlkioWeightDevice) == 0 &&
					len(rc.BlkioDeviceReadBps) == 0 && len(rc.BlkioDeviceWriteBps) == 0 &&
					len(rc.BlkioDeviceReadIOps) == 0 && len(rc.BlkioDeviceWriteIOps) == 0 {
					return nil
				}
				cfg := &ContainerCreateRequestServiceBlkioConfig{
					Weight: rc.BlkioWeight,
				}
				for _, wd := range rc.BlkioWeightDevice {
					cfg.WeightDevice = append(cfg.WeightDevice, BlkioWeightDevice{Path: wd.Path, Weight: wd.Weight})
				}
				for _, t := range rc.BlkioDeviceReadBps {
					cfg.DeviceReadBps = append(cfg.DeviceReadBps, BlkioThrottleDevice{Path: t.Path, Rate: ByteSize(FormatByteSize(int64(t.Rate)))})
				}
				for _, t := range rc.BlkioDeviceWriteBps {
					cfg.DeviceWriteBps = append(cfg.DeviceWriteBps, BlkioThrottleDevice{Path: t.Path, Rate: ByteSize(FormatByteSize(int64(t.Rate)))})
				}
				for _, t := range rc.BlkioDeviceReadIOps {
					cfg.DeviceReadIOps = append(cfg.DeviceReadIOps, BlkioThrottleDevice{Path: t.Path, Rate: ByteSize(strconv.FormatUint(t.Rate, 10))})
				}
				for _, t := range rc.BlkioDeviceWriteIOps {
					cfg.DeviceWriteIOps = append(cfg.DeviceWriteIOps, BlkioThrottleDevice{Path: t.Path, Rate: ByteSize(strconv.FormatUint(t.Rate, 10))})
				}
				return cfg
			}(),
			Gpus: func() GPURequests {
				gpus := GPURequests{}
				for _, dr := range detailedInfo.HostConfig.Resources.DeviceRequests {
					// Only map device requests that carry the implicit gpu
					// capability (docker-compose's gpus == device request).
					hasGPU := false
					for _, caps := range dr.Capabilities {
						for _, c := range caps {
							if c == "gpu" {
								hasGPU = true
							}
						}
					}
					if hasGPU {
						gpus = append(gpus, ContainerCreateRequestGPURequest{
							Driver: dr.Driver,
							Count:  dr.Count,
						})
					}
				}
				return gpus
			}(),

			// StopGracePeriod:  int(detailedInfo.HostConfig.StopGracePeriod.Seconds()),
			
			// Ports
			Ports: func() []string {
					ports := []string{}
					for port, binding := range detailedInfo.NetworkSettings.Ports {
							for _, b := range binding {
									ports = append(ports, fmt.Sprintf("%s:%s:%s/%s", b.HostIP, b.HostPort, port.Port(), port.Proto()))
							}
					}
					return ports
			}(),

			// Volumes
			Volumes: func() []CosmosMount {
					mounts := []CosmosMount{}
					for _, m := range detailedInfo.Mounts {
						cm := CosmosMount{
							Type:   string(m.Type),
							Source: m.Source,
							Target: m.Destination,
						}

						if m.Type == "volume" {
							nodata := strings.Split(strings.TrimSuffix(m.Source, "/_data"), "/")
							cm.Source = nodata[len(nodata)-1]
						}

						mounts = append(mounts, cm)
					}
					return mounts
			}(),
			// Networks
			Networks: func() map[string]ContainerCreateRequestServiceNetwork {
					networks := make(map[string]ContainerCreateRequestServiceNetwork)
					for netName, _ := range detailedInfo.NetworkSettings.Networks {
							networks[netName] = ContainerCreateRequestServiceNetwork{
									// Aliases:     netConfig.Aliases,
									// IPV4Address: netConfig.IPAddress,
									// IPV6Address: netConfig.GlobalIPv6Address,
							}
					}
					return networks
			}(),

			DependsOn:      map[string]ContainerCreateRequestContainerDependsOnCont{},  // This is not directly available from inspect. It's part of docker-compose.
			RestartPolicy:  string(detailedInfo.HostConfig.RestartPolicy.Name),
			Devices:        func() []string {
					var devices []string
					for _, device := range detailedInfo.HostConfig.Devices {
							devices = append(devices, fmt.Sprintf("%s:%s", device.PathOnHost, device.PathInContainer))
					}
					return devices
			}(),
			Expose:         []string{},  // This information might need to be derived from other properties
		}

		// healthcheck
		if detailedInfo.Config.Healthcheck != nil {
			service.HealthCheck.Test = detailedInfo.Config.Healthcheck.Test
			service.HealthCheck.Interval = int(detailedInfo.Config.Healthcheck.Interval.Seconds())
			service.HealthCheck.Timeout = int(detailedInfo.Config.Healthcheck.Timeout.Seconds())
			service.HealthCheck.Retries = detailedInfo.Config.Healthcheck.Retries
			service.HealthCheck.StartPeriod = int(detailedInfo.Config.Healthcheck.StartPeriod.Seconds())
		}

		// user UID/GID
		if detailedInfo.Config.User != "" {
			parts := strings.Split(detailedInfo.Config.User, ":")
			if len(parts) == 2 {
				uid, err := strconv.Atoi(parts[0])
				if err != nil {
					service.UID = uid
				}
				gid, err := strconv.Atoi(parts[1])
				if err != nil {
					service.GID = gid
				}
			}
		}

		//expose 
		// for _, port := range detailedInfo.Config.ExposedPorts {
			
		// }

		return service, nil
}

func ExportDocker() {
	config := utils.GetMainConfig()
	if config.NewInstall {
		return
	}

	ExportError = "" 
	
	errD := Connect()
	if errD != nil {
		ExportError = "Export Docker - cannot connect - " + errD.Error()
		utils.MajorError("ExportDocker - connect - ", errD)
		return
	}

	finalBackup := DockerServiceCreateRequest{}
	
	// List containers
	containers, err := DockerClient.ContainerList(DockerContext, conttype.ListOptions{})
	if err != nil {
		utils.MajorError("ExportDocker - Cannot list containers", err)
		ExportError = "Export Docker - Cannot list containers - " + err.Error()
		return
	}


	// Convert the containers into your custom format
	var services = make(map[string]ContainerCreateRequestContainer)

	for _, container := range containers {
		service, err := ExportContainer(container.ID)
		if err != nil {
			utils.MajorError("ExportDocker - Cannot export container", err)
			return
		}

		containerName := strings.TrimPrefix(service.Name, "/")
		services[containerName] = service
	}

	// List networks
	networks, err := DockerClient.NetworkList(DockerContext, types.NetworkListOptions{})
	if err != nil {
		utils.MajorError("Export Docker - Cannot list networks", err)
		ExportError = "Export Docker - Cannot list networks - " + err.Error()
		return
	}

	finalBackup.Networks = make(map[string]ContainerCreateRequestNetwork)

	// Convert the networks into custom format
	for _, network := range networks {
		if network.Name == "bridge" || network.Name == "host" || network.Name == "none" {
			continue
		}

		// Fetch detailed info of each network
		detailedInfo, err := DockerClient.NetworkInspect(DockerContext, network.ID, types.NetworkInspectOptions{})
		if err != nil {
			utils.MajorError("Export Docker - Cannot inspect network", err)
			ExportError = "Export Docker - Cannot inspect network - " + err.Error()
			return
		}

		// Map the detailedInfo to ContainerCreateRequestContainer struct
		network := ContainerCreateRequestNetwork{
			Name:         detailedInfo.Name,
			Driver:       detailedInfo.Driver,
			Internal:     detailedInfo.Internal,
			Attachable:   detailedInfo.Attachable,
			EnableIPv6:   detailedInfo.EnableIPv6,
			Labels:       detailedInfo.Labels,
		}

		network.IPAM.Driver = detailedInfo.IPAM.Driver
		for _, config := range detailedInfo.IPAM.Config {
			network.IPAM.Config = append(network.IPAM.Config, ContainerCreateRequestNetworkIPAMConfig{
				Subnet:  config.Subnet,
				Gateway: config.Gateway,
			})
		}

		finalBackup.Networks[detailedInfo.Name] = network
	}

	// remove cosmos from services
	if utils.IsInsideContainer {
		cosmos := services[os.Getenv("HOSTNAME")]
		delete(services, os.Getenv("HOSTNAME"))

		// export separately cosmos
		// Create a buffer to hold the JSON output
		var buf bytes.Buffer

		// Create a new yaml encoder that writes to the buffer
		encoder := yaml.NewEncoder(&buf)

		// Set escape HTML to false to avoid escaping special characters
		// encoder.SetEscapeHTML(false)
		//format
		// encoder.SetIndent("", "  ")

		// Use the encoder to write the structured data to the buffer
		toExport := map[string]map[string]ContainerCreateRequestContainer {
			"services": map[string]ContainerCreateRequestContainer {
				os.Getenv("HOSTNAME"): cosmos,
			},
		}

		err = encoder.Encode(toExport)
		if err != nil {
				utils.MajorError("Export Docker - Cannot marshal docker backup", err)
				ExportError = "Export Docker - Cannot marshal docker backup - " + err.Error()
		}

		// The JSON data is now in buf.Bytes()
		yamlData := buf.Bytes()

		// Write the JSON data to a file
		err = ioutil.WriteFile(utils.CONFIGFOLDER + "cosmos.docker-compose.yaml", yamlData, 0600)
		if err != nil {
				utils.MajorError("Export Docker - Cannot save docker backup", err)
				ExportError = "Export Docker - Cannot save docker backup - " + err.Error()
		}
	}

	// Convert the services map to your finalBackup struct
	finalBackup.Services = services

	// Create a buffer to hold the JSON output
	var buf bytes.Buffer

	// Create a new JSON encoder that writes to the buffer
	encoder := json.NewEncoder(&buf)

	// Set escape HTML to false to avoid escaping special characters
	encoder.SetEscapeHTML(false)
	//format
	encoder.SetIndent("", "  ")

	// Use the encoder to write the structured data to the buffer
	err = encoder.Encode(finalBackup)
	if err != nil {
			utils.MajorError("Export Docker - Cannot marshal docker backup", err)
			ExportError = "Export Docker - Cannot marshal docker backup - " + err.Error()
	}

	// The JSON data is now in buf.Bytes()
	jsonData := buf.Bytes()

	// Write the JSON data to a file
	err = ioutil.WriteFile(utils.CONFIGFOLDER + "backup.cosmos-compose.json", jsonData, 0600)
	if err != nil {
		utils.MajorError("Export Docker - Cannot save docker backup", err)
		ExportError = "Export Docker - Cannot save docker backup - " + err.Error()
	}
}