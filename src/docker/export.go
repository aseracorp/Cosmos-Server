package docker

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"time"
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

// FormatDuration converts a time.Duration (as reported by the docker daemon's
// container healthcheck config) into a docker-compose-style duration string
// such as "15s" or "1m30s". This keeps healthcheck duration fields (interval,
// timeout, start_period) consistent with the format docker-compose expects, so
// round-tripped exports behave the same as the originals. Zero and negative
// durations yield "" (absent), matching how compose omits unset fields.
func FormatDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	ns := d.Nanoseconds()
	units := []struct {
		size time.Duration
		sym  string
	}{
		{time.Hour, "h"},
		{time.Minute, "m"},
		{time.Second, "s"},
		{time.Millisecond, "ms"},
		{time.Microsecond, "us"},
		{time.Nanosecond, "ns"},
	}
	var b strings.Builder
	for _, u := range units {
		if ns >= int64(u.size) {
			n := ns / int64(u.size)
			b.WriteString(strconv.FormatInt(n, 10))
			b.WriteString(u.sym)
			ns %= int64(u.size)
		}
	}
	if b.Len() == 0 {
		return "0s"
	}
	return b.String()
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

			// depends_on is reconstructed from the compose label (stripped from
			// Labels below) so the *field* is the source of truth for the user.
			DependsOn:      DependsOnFieldFromLabels(detailedInfo.Config, buildContainerNameIndex()),
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
			service.HealthCheck = &ContainerCreateRequestContainerHealthcheck{
				Test:        detailedInfo.Config.Healthcheck.Test,
				Interval:    DurationStr(FormatDuration(detailedInfo.Config.Healthcheck.Interval)),
				Timeout:     DurationStr(FormatDuration(detailedInfo.Config.Healthcheck.Timeout)),
				Retries:     detailedInfo.Config.Healthcheck.Retries,
				StartPeriod: DurationStr(FormatDuration(detailedInfo.Config.Healthcheck.StartPeriod)),
			}
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

		// hide the internal depends_on label; the field is the source of truth
		service.Labels = stripInternalDependsOnLabel(service.Labels)

		return service, nil
}

// ExportContainerRuntime exports the service definition containing only the
// settings that were EXPLICITLY set when the container was created, as
// opposed to values implicitly inherited from the image's Dockerfile.
//
// It does this by diffing the running container's config (ContainerInspect)
// against the image's own config (ImageInspectWithRaw):
//   - env vars: only entries that are not present in the image env, or whose
//     value differs from the image default, are kept (image-inherited ENV and
//     Cosmos-injected TZ/normalized vars are dropped).
//   - labels: only labels that are not in the image labels are kept, then
//     Cosmos bookkeeping labels (cosmos.* / com.docker.compose.*) are dropped.
//   - command / entrypoint: kept only when they differ from the image's.
//   - working dir / user / stop signal / hostname / domainname / mac address:
//     kept only when they differ from the image's.
//   - healthcheck: kept only when it differs from the image's.
//
// Fields that are inherently runtime (ports, volumes, networks, resource
// limits, restart policy, dns, etc.) have no image-config baseline and are
// exported as-is — they represent the container's actual wiring.
//
// If the image cannot be inspected (deleted, retagged, or not present
// locally), we fall back to the full runtime export so the compose editor
// still gets a usable document.
func ExportContainerRuntime(containerID string) (ContainerCreateRequestContainer, error) {
	service, err := ExportContainer(containerID)
	if err != nil {
		return service, err
	}

	// Fetch detailed info again to get the container config + resolve the image.
	detailedInfo, err := DockerClient.ContainerInspect(DockerContext, containerID)
	if err != nil {
		ExportError = "Export Docker - Cannot inspect container" + containerID + " - " + err.Error()
		return ContainerCreateRequestContainer{}, errors.New(ExportError)
	}
	if detailedInfo.Config == nil {
		return service, nil
	}

	image, _, imgErr := DockerClient.ImageInspectWithRaw(DockerContext, detailedInfo.Config.Image)
	if imgErr != nil || image.Config == nil {
		// Image unavailable — cannot diff; return the full runtime export.
		return service, nil
	}
	imgConfig := image.Config

	// Environment: keep user-set / overridden entries only.
	if len(imgConfig.Env) > 0 {
		imgEnv := map[string]string{}
		for _, e := range imgConfig.Env {
			parts := strings.SplitN(e, "=", 2)
			if len(parts) == 2 {
				imgEnv[parts[0]] = parts[1]
			}
		}
		filtered := []string{}
		for _, e := range service.Environment {
			parts := strings.SplitN(e, "=", 2)
			if len(parts) != 2 {
				filtered = append(filtered, e)
				continue
			}
			k, v := parts[0], parts[1]
			if imgV, present := imgEnv[k]; !present || imgV != v {
				filtered = append(filtered, e)
			}
		}
		service.Environment = filtered
	}

	// Labels: keep only labels not in the image, then drop Cosmos-internal ones.
	// The image-inheritance filter only applies to *user* labels: cosmos.* and
	// cosmos-* labels are Cosmos bookkeeping that can never be a Dockerfile
	// default (e.g. cosmos.stack, cosmos.stack.main, and their legacy dash
	// aliases cosmos-stack / cosmos-stack-main) and must always be shown, while
	// com.docker.compose.* labels are recomputed from the compose fields and
	// kept hidden.
	if len(imgConfig.Labels) > 0 {
		filtered := map[string]string{}
		for k, v := range service.Labels {
			// Cosmos-managed labels (dot or dash) are never image defaults —
			// always expose (covers cosmos.stack, cosmos-stack,
			// cosmos.stack.main, cosmos-stack-main, ...).
			if strings.HasPrefix(k, "cosmos.") || strings.HasPrefix(k, "cosmos-") {
				filtered[k] = v
				continue
			}
			// Internal compose bookkeeping is recomputed from fields — hide.
			if strings.HasPrefix(k, "com.docker.compose.") {
				continue
			}
			// inherited from image → not user-set
			if imgV, present := imgConfig.Labels[k]; present && imgV == v {
				continue
			}
			filtered[k] = v
		}
		service.Labels = filtered
	}

	// Command / Entrypoint: keep only if they differ from the image's.
	containerCmd := strings.Join(detailedInfo.Config.Cmd, " ")
	imgCmd := strings.Join(imgConfig.Cmd, " ")
	if containerCmd != "" && containerCmd == imgCmd {
		service.Command = nil
	}
	containerEntry := strings.Join(detailedInfo.Config.Entrypoint, " ")
	imgEntry := strings.Join(imgConfig.Entrypoint, " ")
	if containerEntry != "" && containerEntry == imgEntry {
		service.Entrypoint = nil
	}

	// WorkingDir / User / StopSignal / Hostname / Domainname / MacAddress.
	if imgConfig.WorkingDir != "" && detailedInfo.Config.WorkingDir == imgConfig.WorkingDir {
		service.WorkingDir = ""
	}
	if imgConfig.User != "" && detailedInfo.Config.User == imgConfig.User {
		service.User = ""
		service.UID = 0
		service.GID = 0
	}
	if imgConfig.StopSignal != "" && detailedInfo.Config.StopSignal == imgConfig.StopSignal {
		service.StopSignal = ""
	}
	if imgConfig.Hostname != "" && detailedInfo.Config.Hostname == imgConfig.Hostname {
		service.Hostname = ""
	}
	if imgConfig.Domainname != "" && detailedInfo.Config.Domainname == imgConfig.Domainname {
		service.Domainname = ""
	}

	// Healthcheck: keep only if it differs from the image's.
	if imgConfig.Healthcheck != nil {
		if detailedInfo.Config.Healthcheck != nil &&
			strings.Join(detailedInfo.Config.Healthcheck.Test, " ") == strings.Join(imgConfig.Healthcheck.Test, " ") &&
			detailedInfo.Config.Healthcheck.Interval == imgConfig.Healthcheck.Interval &&
			detailedInfo.Config.Healthcheck.Timeout == imgConfig.Healthcheck.Timeout &&
			detailedInfo.Config.Healthcheck.Retries == imgConfig.Healthcheck.Retries &&
			detailedInfo.Config.Healthcheck.StartPeriod == imgConfig.Healthcheck.StartPeriod {
			service.HealthCheck = nil
		}
	}

	// Runtime: hide when it matches the docker daemon's default runtime.
	// The default is read from the local daemon (Info.DefaultRuntime, usually
	// "runc" but administrators can configure a different default), so a
	// container running on the standard runtime is not shown as if the user
	// explicitly chose it. Empty means "use the daemon default".
	if service.Runtime == "" || service.Runtime == getDockerDefaultRuntime() {
		service.Runtime = ""
	}

	// Shm size: hide when the container runs with the docker daemon's default
	// shm size. The default is read from the local daemon
	// (getDockerDefaultShmSize), so a configuration that changes
	// default-shm-size is respected; 0/unset also means "use the daemon
	// default". Only an explicit shm_size that differs from the daemon's
	// default is a user-set value.
	shm := detailedInfo.HostConfig.ShmSize
	if shm == 0 || shm == getDockerDefaultShmSize() {
		service.ShmSize = ""
	}

	return service, nil
}

// getDockerDefaultRuntime returns the docker daemon's default runtime (e.g.
// "runc") by querying the local daemon Info, cached after the first call. An
// empty result falls back to the conventional "runc".
var cachedDefaultRuntime = ""
var cachedDefaultRuntimeSet = false

func getDockerDefaultRuntime() string {
	if cachedDefaultRuntimeSet {
		return cachedDefaultRuntime
	}
	defaultRuntime := "runc"
	if DockerClient != nil {
		if info, err := DockerClient.Info(DockerContext); err == nil {
			if info.DefaultRuntime != "" {
				defaultRuntime = info.DefaultRuntime
			}
		}
	}
	cachedDefaultRuntime = defaultRuntime
	cachedDefaultRuntimeSet = true
	return defaultRuntime
}

// getDockerDefaultShmSize returns the docker daemon's configured default shm
// size (bytes) by querying the daemon "/info" endpoint (read from the local
// docker daemon). We use the raw response rather than system.Info because the
// client's Info struct does not carry the shm field even though the daemon
// applies it at container-create time (0 -> configured default, default 64MB).
// Falls back to Docker's documented constant (64MB) when the daemon does not
// expose the value and we cannot reach it.
var cachedDefaultShmSize int64 = 0
var cachedDefaultShmSizeSet = false

func getDockerDefaultShmSize() int64 {
	if cachedDefaultShmSizeSet {
		return cachedDefaultShmSize
	}
	shm := int64(64 * 1024 * 1024) // Docker DefaultShmSize
	if DockerClient != nil {
		host := DockerClient.DaemonHost()
		if host != "" {
			// DaemonHost returns e.g. unix:///var/run/docker.sock or
			// tcp://host:port. Build a URL that the client's HTTP transport
			// (already configured for unix/tls) can serve.
			reqURL := strings.TrimRight(host, "/") + "/info"
			if req, reqErr := http.NewRequest("GET", reqURL, nil); reqErr == nil {
				if resp, err := DockerClient.HTTPClient().Do(req); err == nil {
					if resp != nil && resp.Body != nil {
						defer resp.Body.Close()
						var infoMap map[string]interface{}
						if decErr := json.NewDecoder(resp.Body).Decode(&infoMap); decErr == nil {
							for _, key := range []string{"DefaultShmSize", "ShmSize", "default-shm-size"} {
								if v, ok := infoMap[key]; ok {
									if num, numOK := toInt64(v); numOK && num > 0 {
										shm = num
										break
									}
								}
							}
						}
					}
				}
			}
		}
	}
	cachedDefaultShmSize = shm
	cachedDefaultShmSizeSet = true
	return shm
}

// toInt64 coerces a JSON-typed value (float64, string, or json.Number) to an
// int64, reporting whether the conversion was possible.
func toInt64(v interface{}) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int64:
		return n, true
	case int:
		return int64(n), true
	case string:
		var parsed int64
		if _, err := fmt.Sscanf(n, "%d", &parsed); err == nil {
			return parsed, true
		}
	case json.Number:
		if parsed, err := n.Int64(); err == nil {
			return parsed, true
		}
	}
	return 0, false
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