package docker

import (
	"encoding/json"
	"net/http"
	"os"
	"fmt"
	"strings"
	"errors"

	"github.com/azukaar/cosmos-server/src/utils"
	containerType "github.com/docker/docker/api/types/container"
	"github.com/docker/go-connections/nat"
	"github.com/docker/go-units"
	"github.com/gorilla/mux"
)

type ContainerForm struct {
	Image          string            `json:"image"`
	User 				   string    `json:"user"`
	RestartPolicy  string            `json:"restartPolicy"`
	Env            []string          `json:"envVars"`
	Devices        []string          `json:"devices"`
	Labels         map[string]string `json:"labels"`
	PortBindings   nat.PortMap       `json:"portBindings"`
	Volumes        []CosmosMount     `json:"volumes"`
	// we make this a int so that we can ignore 0
	Interactive    int               `json:"interactive"`
	NetworkMode 	 string           `json:"networkMode"`
	// DNS replaces the DNS servers of the container when sent, an empty list for the default of Docker
	DNS            []string          `json:"dns"`
	// MemLimit / MemSwapLimit / MemReservation / CPUs are pointers: absent
	// means the constraint is left as it is, the convention this form already
	// uses for DNS / NetworkMode / Volumes. MemSwapLimit is the TOTAL of memory
	// + swap (docker-compose's memswap_limit), "-1" means unlimited swap.
	MemLimit *string  `json:"memLimit"`
	MemSwapLimit *string `json:"memSwapLimit"`
	MemReservation *string `json:"memReservation"`
	CPUs     *float64 `json:"cpus"`
}

// applyResourceConstraints writes the memory / CPU limits carried by the form
// into an existing HostConfig. Fields that the request does not carry are
// left exactly as they are, so a partial update (say, env vars only) cannot
// wipe limits the compose blueprint set.
//
// The byte values are already parsed; a nil pointer means the field was absent
// from the request. The daemon's own rules are applied against the state we
// are about to submit, not against this request on its own, so a pair that the
// daemon would reject fails here with a clean error instead of failing the
// container recreate: mem_swap_limit >= mem_limit, and mem_limit >= reservation.
func applyResourceConstraints(resources *containerType.Resources, memLimitBytes, memSwapBytes, memResBytes *int64, nanoCPUs *int64) error {
	newMem := resources.Memory
	if memLimitBytes != nil {
		newMem = *memLimitBytes
	}
	newSwap := resources.MemorySwap
	if memSwapBytes != nil {
		newSwap = *memSwapBytes
	}
	newRes := resources.MemoryReservation
	if memResBytes != nil {
		newRes = *memResBytes
	}

	if newMem > 0 && newSwap > 0 && newSwap < newMem {
		return errors.New("Memory swap limit can not be smaller than the memory limit")
	}
	if newMem > 0 && newRes > 0 && newRes > newMem {
		return errors.New("Memory reservation can not be larger than the memory limit")
	}

	if memLimitBytes != nil {
		resources.Memory = newMem
	}
	if memSwapBytes != nil {
		resources.MemorySwap = newSwap
	}
	if memResBytes != nil {
		resources.MemoryReservation = newRes
	}
	if nanoCPUs != nil {
		resources.NanoCPUs = *nanoCPUs
	}
	return nil
}

// parseResourceForm turns the form's string/float limits into the raw byte /
// nano-CPU values the daemon wants, reporting absence as nil.
func parseResourceForm(form ContainerForm) (memLimitBytes, memSwapBytes, memResBytes *int64, nanoCPUs *int64, err error) {
	// A byte-size field: "" clears the limit, "-1" is unlimited swap, anything
	// else goes through RAMInBytes like CreateService parses compose byte sizes.
	parse := func(field *string, name string, allowUnlimited bool) (*int64, error) {
		if field == nil {
			return nil, nil
		}
		if *field == "" {
			v := int64(0)
			return &v, nil
		}
		if allowUnlimited && *field == "-1" {
			v := int64(-1)
			return &v, nil
		}
		parsed, perr := units.RAMInBytes(*field)
		if perr != nil {
			return nil, fmt.Errorf("invalid %s value: %s", name, *field)
		}
		return &parsed, nil
	}

	if memLimitBytes, err = parse(form.MemLimit, "mem_limit", false); err != nil {
		return nil, nil, nil, nil, err
	}
	if memSwapBytes, err = parse(form.MemSwapLimit, "memswap_limit", true); err != nil {
		return nil, nil, nil, nil, err
	}
	if memResBytes, err = parse(form.MemReservation, "mem_reservation", false); err != nil {
		return nil, nil, nil, nil, err
	}
	if form.CPUs != nil {
		v := int64(*form.CPUs * 1e9)
		nanoCPUs = &v
	}
	return memLimitBytes, memSwapBytes, memResBytes, nanoCPUs, nil
}

// UpdateContainerRoute godoc
// @Summary Update a Docker container's settings
// @Tags docker
// @Accept json
// @Produce json
// @Param containerId path string true "Container ID or name"
// @Param body body ContainerForm true "Container update payload"
// @Security BearerAuth
// @Success 200 {object} utils.APIResponse
// @Failure 400 {object} utils.HTTPErrorResult
// @Failure 403 {object} utils.HTTPErrorResult
// @Failure 500 {object} utils.HTTPErrorResult
// @Router /api/servapps/{containerId}/update [post]
func UpdateContainerRoute(w http.ResponseWriter, req *http.Request) {
	if utils.CheckPermissions(w, req, utils.PERM_RESOURCES) != nil {
		return
	}

	utils.Log("UpdateContainer" + "Updating container")

	if req.Method == "POST" {
		errD := Connect()
		if errD != nil {
			utils.Error("UpdateContainer", errD)
			utils.HTTPError(w, "Internal server error: "+errD.Error(), http.StatusInternalServerError, "DS002")
			return
		}

		vars := mux.Vars(req)
		containerName := utils.SanitizeSafe(vars["containerId"])
		
		if utils.IsInsideContainer && containerName == os.Getenv("HOSTNAME") {
			utils.Error("SecureContainerRoute - Container cannot update itself", nil)
			utils.HTTPError(w, "Container cannot update itself", http.StatusBadRequest, "DS003")
			return
		}

		container, err := DockerClient.ContainerInspect(DockerContext, containerName)
		if err != nil {
			utils.Error("UpdateContainer", err)
			utils.HTTPError(w, "Internal server error: "+err.Error(), http.StatusInternalServerError, "DS002")
			return
		}

		var form ContainerForm
		err = json.NewDecoder(req.Body).Decode(&form)
		if err != nil {
			utils.Error("UpdateContainer", err)
			utils.HTTPError(w, "Invalid JSON", http.StatusBadRequest, "DS003")
			return
		}

		// Update container settings
		if(form.Image != "") {
			container.Config.Image = form.Image
		}
		if(form.RestartPolicy != "") {
			// THIS IS HACK BECAUSE USER IS NULLABLE, BETTER SOLUTION TO COME
			container.Config.User = form.User
			container.HostConfig.RestartPolicy = containerType.RestartPolicy{Name: containerType.RestartPolicyMode(form.RestartPolicy)}
		}
		if(form.Env != nil) {
			container.Config.Env = form.Env
		}
		
		if(form.Devices != nil) {
			container.HostConfig.Devices = []containerType.DeviceMapping{}
			for _, device := range form.Devices {
				deviceHost := strings.Split(device, ":")[0]
				deviceCont := deviceHost

				if strings.Contains(device, ":") {
					deviceCont = strings.Split(device, ":")[1]
				}

				container.HostConfig.Devices = append(container.HostConfig.Devices, containerType.DeviceMapping{
					PathOnHost:        deviceHost,
					PathInContainer:   deviceCont,
					CgroupPermissions: "rwm",
				})
			}
		}

		if(form.Labels != nil) {
			container.Config.Labels = form.Labels
		}

		if(form.PortBindings != nil) {
			utils.Debug(fmt.Sprintf("UpdateContainer: PortBindings: %v", form.PortBindings))
			container.HostConfig.PortBindings = form.PortBindings
			container.Config.ExposedPorts = make(map[nat.Port]struct{})
			for port := range form.PortBindings {
				container.Config.ExposedPorts[port] = struct{}{}
			}
		}
		if(form.Volumes != nil) {
			container.HostConfig.Mounts = ToDockerMountSlice(form.Volumes)
			container.HostConfig.Binds = []string{}
		}
		if(form.Interactive != 0) {
			container.Config.Tty = form.Interactive == 2
			container.Config.OpenStdin = form.Interactive == 2
		}
		if(form.NetworkMode != "") {
			// normalize container/service refs to stable container:<name>
			networkMode := ContainerRefToName(form.NetworkMode)
			container.HostConfig.NetworkMode = containerType.NetworkMode(networkMode)
			// if not bridge, remove mac address
			if networkMode != "bridge" &&
				 networkMode != "default" {
					container.Config.MacAddress = ""
			}
			// update cosmos-force-network-mode label
			if container.Config.Labels == nil {
				container.Config.Labels = make(map[string]string)
			}
			container.Config.Labels["cosmos-force-network-mode"] = networkMode
		}
		if form.DNS != nil {
			container.HostConfig.DNS = form.DNS
		}

		// Resource constraints. Absent fields are left untouched (see the
		// ContainerForm comment): an update that only changes env vars must
		// not silently wipe the memory/CPU limits the compose blueprint set.
		memLimitBytes, memSwapBytes, memResBytes, nanoCPUs, errRes := parseResourceForm(form)
		if errRes != nil {
			utils.Error("UpdateContainer: Invalid resource limit", errRes)
			utils.HTTPError(w, errRes.Error(), http.StatusBadRequest, "DS003")
			return
		}
		if errRes := applyResourceConstraints(&container.HostConfig.Resources, memLimitBytes, memSwapBytes, memResBytes, nanoCPUs); errRes != nil {
			utils.Error("UpdateContainer: Incompatible resource limits", errRes)
			utils.HTTPError(w, errRes.Error(), http.StatusBadRequest, "DS003")
			return
		}

		// docker must not auto-restart a container Cosmos put to sleep
		if IsLazyLabels(container.Config.Labels) {
			container.HostConfig.RestartPolicy = containerType.RestartPolicy{Name: containerType.RestartPolicyMode("no")}
		}

		_, err = EditContainer(container.ID, container, false)
		if err != nil {
			utils.Error("UpdateContainer: EditContainer", err)
			utils.HTTPError(w, "Internal server error: "+err.Error(), http.StatusInternalServerError, "DS004")
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "OK",
		})
	} else {
		utils.Error("UpdateContainer: Method not allowed " + req.Method, nil)
		utils.HTTPError(w, "Method not allowed", http.StatusMethodNotAllowed, "HTTP001")
		return
	}
}