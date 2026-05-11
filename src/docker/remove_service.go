package docker

import (
	"fmt"
	"sync"
	"time"

	conttype "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	voltype "github.com/docker/docker/api/types/volume"
	doctype "github.com/docker/docker/api/types"

	"github.com/azukaar/cosmos-server/src/utils"
)

// DeploymentLabel is the docker label key used to identify resources that
// belong to a scheduler-managed deployment. The value is the deployment name.
const DeploymentLabel = "cosmos-deployment"

// RemoveByDeploymentLabel discovers and tears down every container, network, and
// volume labeled cosmos-deployment=<deploymentName>. Mirrors the sequence the
// servapps delete modal uses (containers → 1s pause → networks + volumes in
// parallel). Per-resource failures are collected, not fatal — partial teardown
// is still progress.
func RemoveByDeploymentLabel(deploymentName string, OnLog func(string)) []error {
	if OnLog == nil {
		OnLog = func(string) {}
	}

	if err := Connect(); err != nil {
		utils.Error("[SCHED-NODE] RemoveByDeploymentLabel: docker connect failed", err)
		return []error{err}
	}

	labelFilter := filters.NewArgs()
	labelFilter.Add("label", DeploymentLabel+"="+deploymentName)

	var errs []error

	// 1. Containers: kill (best-effort) then remove with Force.
	containers, err := DockerClient.ContainerList(DockerContext, conttype.ListOptions{
		All:     true,
		Filters: labelFilter,
	})
	if err != nil {
		utils.Error("[SCHED-NODE] RemoveByDeploymentLabel: ContainerList failed for "+deploymentName, err)
		OnLog(utils.DoErr("Failed to list containers for deployment %s: %s\n", deploymentName, err.Error()))
		errs = append(errs, err)
	}

	utils.Log(fmt.Sprintf("[SCHED-NODE] remove deployment=%s (label filter) containers=%d", deploymentName, len(containers)))
	OnLog(fmt.Sprintf("Removing deployment %s: %d container(s)\n", deploymentName, len(containers)))

	for _, c := range containers {
		name := c.ID
		if len(c.Names) > 0 {
			name = c.Names[0]
		}

		// Kill is best-effort: a stopped container will return an error we can ignore.
		if killErr := DockerClient.ContainerKill(DockerContext, c.ID, "SIGKILL"); killErr != nil {
			utils.Debug("[SCHED-NODE] ContainerKill " + name + ": " + killErr.Error())
		}

		if rmErr := DockerClient.ContainerRemove(DockerContext, c.ID, conttype.RemoveOptions{Force: true}); rmErr != nil {
			utils.Warn("[SCHED-NODE] failed to remove container " + name + ": " + rmErr.Error())
			OnLog(utils.DoWarn("Failed to remove container %s: %s\n", name, rmErr.Error()))
			errs = append(errs, rmErr)
		} else {
			utils.Debug("[SCHED-NODE] removed container " + name)
			OnLog(fmt.Sprintf("Removed container %s\n", name))
		}
	}

	// 2. Pause to let docker release references — matches the UI's setTimeout(1000).
	time.Sleep(1 * time.Second)

	// 3. Networks and volumes in parallel.
	var wg sync.WaitGroup
	var errMu sync.Mutex

	wg.Add(1)
	go func() {
		defer wg.Done()

		networks, nlErr := DockerClient.NetworkList(DockerContext, doctype.NetworkListOptions{Filters: labelFilter})
		if nlErr != nil {
			utils.Error("[SCHED-NODE] RemoveByDeploymentLabel: NetworkList failed for "+deploymentName, nlErr)
			OnLog(utils.DoErr("Failed to list networks for deployment %s: %s\n", deploymentName, nlErr.Error()))
			errMu.Lock()
			errs = append(errs, nlErr)
			errMu.Unlock()
			return
		}

		for _, n := range networks {
			if rmErr := DockerClient.NetworkRemove(DockerContext, n.ID); rmErr != nil {
				utils.Warn("[SCHED-NODE] failed to remove network " + n.Name + ": " + rmErr.Error())
				OnLog(utils.DoWarn("Failed to remove network %s: %s\n", n.Name, rmErr.Error()))
				errMu.Lock()
				errs = append(errs, rmErr)
				errMu.Unlock()
			} else {
				utils.Debug("[SCHED-NODE] removed network " + n.Name)
				OnLog(fmt.Sprintf("Removed network %s\n", n.Name))
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()

		volumes, vlErr := DockerClient.VolumeList(DockerContext, voltype.ListOptions{Filters: labelFilter})
		if vlErr != nil {
			utils.Error("[SCHED-NODE] RemoveByDeploymentLabel: VolumeList failed for "+deploymentName, vlErr)
			OnLog(utils.DoErr("Failed to list volumes for deployment %s: %s\n", deploymentName, vlErr.Error()))
			errMu.Lock()
			errs = append(errs, vlErr)
			errMu.Unlock()
			return
		}

		for _, v := range volumes.Volumes {
			if v == nil {
				continue
			}
			if rmErr := DockerClient.VolumeRemove(DockerContext, v.Name, true); rmErr != nil {
				utils.Warn("[SCHED-NODE] failed to remove volume " + v.Name + ": " + rmErr.Error())
				OnLog(utils.DoWarn("Failed to remove volume %s: %s\n", v.Name, rmErr.Error()))
				errMu.Lock()
				errs = append(errs, rmErr)
				errMu.Unlock()
			} else {
				utils.Debug("[SCHED-NODE] removed volume " + v.Name)
				OnLog(fmt.Sprintf("Removed volume %s\n", v.Name))
			}
		}
	}()

	wg.Wait()

	utils.Log(fmt.Sprintf("[SCHED-NODE] remove deployment=%s done errors=%d", deploymentName, len(errs)))
	OnLog(fmt.Sprintf("Remove deployment %s complete (%d errors)\n", deploymentName, len(errs)))

	return errs
}

// ContainerIDsByDeploymentLabel returns the container IDs currently labeled
// cosmos-deployment=<name>. Used by the scheduler's waitForRunning to poll
// container state after an apply.
func ContainerIDsByDeploymentLabel(deploymentName string) ([]string, error) {
	if err := Connect(); err != nil {
		return nil, err
	}

	labelFilter := filters.NewArgs()
	labelFilter.Add("label", DeploymentLabel+"="+deploymentName)

	containers, err := DockerClient.ContainerList(DockerContext, conttype.ListOptions{
		All:     true,
		Filters: labelFilter,
	})
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(containers))
	for _, c := range containers {
		ids = append(ids, c.ID)
	}
	return ids, nil
}

// ListDeploymentNamesRunningHere returns the distinct deployment names found on
// containers labeled with DeploymentLabel on this host. Called from the heartbeat
// loop to populate NodeHeartbeat.RunningDeployments. Docker is authoritative —
// no in-memory state is consulted.
func ListDeploymentNamesRunningHere() ([]string, error) {
	if err := Connect(); err != nil {
		return nil, err
	}

	labelFilter := filters.NewArgs()
	labelFilter.Add("label", DeploymentLabel)

	containers, err := DockerClient.ContainerList(DockerContext, conttype.ListOptions{
		All:     true,
		Filters: labelFilter,
	})
	if err != nil {
		return nil, err
	}

	seen := map[string]struct{}{}
	for _, c := range containers {
		if name := c.Labels[DeploymentLabel]; name != "" {
			seen[name] = struct{}{}
		}
	}

	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	return names, nil
}

// ContainerIsRunning returns true when the given container ID is in the "running"
// Docker state. Used by the scheduler's waitForRunning polling loop.
func ContainerIsRunning(containerID string) (bool, error) {
	if err := Connect(); err != nil {
		return false, err
	}

	insp, err := DockerClient.ContainerInspect(DockerContext, containerID)
	if err != nil {
		return false, err
	}
	if insp.State == nil {
		return false, nil
	}
	return insp.State.Running, nil
}

