package control

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const (
	environmentKindKubernetes     = "kubernetes"
	environmentKindVirtualMachine = "virtual_machine"
)

func normalizeEnvironmentKind(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", environmentKindKubernetes:
		return environmentKindKubernetes
	case environmentKindVirtualMachine, "vm", "virtual-machine":
		return environmentKindVirtualMachine
	default:
		return ""
	}
}

func normalizeEnvironmentStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "configured":
		return "configured"
	case "ready", "unreachable":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func normalizeEnvironmentInput(existing Environment, input EnvironmentInput) (Environment, error) {
	if input.VirtualMachine != nil {
		input.SSHHost = firstNonEmpty(input.SSHHost, input.VirtualMachine.SSHHost)
		if input.SSHPort == 0 {
			input.SSHPort = input.VirtualMachine.SSHPort
		}
		input.SSHUser = firstNonEmpty(input.SSHUser, input.VirtualMachine.SSHUser)
		input.SSHAuthRef = firstNonEmpty(input.SSHAuthRef, input.VirtualMachine.SSHAuthRef)
		input.Workdir = firstNonEmpty(input.Workdir, input.VirtualMachine.Workdir)
		input.ServiceHint = firstNonEmpty(input.ServiceHint, input.VirtualMachine.ServiceHint)
		if len(input.Labels) == 0 {
			input.Labels = input.VirtualMachine.Labels
		}
	}
	kind := normalizeEnvironmentKind(firstNonEmpty(input.Kind, existing.Kind))
	if kind == "" {
		return Environment{}, errors.New("environment kind must be kubernetes or virtual_machine")
	}
	status := normalizeEnvironmentStatus(firstNonEmpty(input.Status, existing.Status))
	if status == "" {
		return Environment{}, errors.New("environment status must be configured, ready, or unreachable")
	}
	name := collapseWhitespace(firstNonEmpty(input.Name, existing.Name))
	if name == "" {
		return Environment{}, errors.New("environment name is required")
	}
	environment := existing
	environment.Name = name
	environment.Kind = kind
	environment.Status = status
	switch kind {
	case environmentKindKubernetes:
		return environment, nil
	case environmentKindVirtualMachine:
		sshPort := input.SSHPort
		if sshPort == 0 && existing.VirtualMachine != nil {
			sshPort = existing.VirtualMachine.SSHPort
		}
		if sshPort == 0 {
			sshPort = 22
		}
		if sshPort < 1 || sshPort > 65535 {
			return Environment{}, errors.New("sshPort must be between 1 and 65535")
		}
		vm := &VirtualMachineEnvironmentConfig{
			SSHHost:     strings.TrimSpace(firstNonEmpty(input.SSHHost, existingVirtualMachineField(existing, "sshHost"))),
			SSHPort:     sshPort,
			SSHUser:     strings.TrimSpace(firstNonEmpty(input.SSHUser, existingVirtualMachineField(existing, "sshUser"))),
			SSHAuthRef:  strings.TrimSpace(firstNonEmpty(input.SSHAuthRef, existingVirtualMachineField(existing, "sshAuthRef"))),
			Workdir:     strings.TrimSpace(firstNonEmpty(input.Workdir, existingVirtualMachineField(existing, "workdir"))),
			ServiceHint: strings.TrimSpace(firstNonEmpty(input.ServiceHint, existingVirtualMachineField(existing, "serviceHint"))),
		}
		labels, err := normalizeJSONObjectPayload(input.Labels)
		if len(input.Labels) == 0 && existing.VirtualMachine != nil {
			labels = copyRawMessage(existing.VirtualMachine.Labels)
			err = nil
		}
		if err != nil {
			return Environment{}, err
		}
		vm.Labels = labels
		if vm.SSHHost == "" {
			return Environment{}, errors.New("sshHost is required for virtual machine environments")
		}
		if vm.SSHUser == "" {
			return Environment{}, errors.New("sshUser is required for virtual machine environments")
		}
		environment.VirtualMachine = vm
		environment.Kubernetes = nil
		return environment, nil
	default:
		return Environment{}, errors.New("environment kind must be kubernetes or virtual_machine")
	}
}

func existingVirtualMachineField(environment Environment, field string) string {
	if environment.VirtualMachine == nil {
		return ""
	}
	switch field {
	case "sshHost":
		return environment.VirtualMachine.SSHHost
	case "sshUser":
		return environment.VirtualMachine.SSHUser
	case "sshAuthRef":
		return environment.VirtualMachine.SSHAuthRef
	case "workdir":
		return environment.VirtualMachine.Workdir
	case "serviceHint":
		return environment.VirtualMachine.ServiceHint
	default:
		return ""
	}
}

func clusterInputFromEnvironmentInput(input EnvironmentInput) ClusterInput {
	cluster := ClusterInput{
		Name:                input.Name,
		Status:              input.Status,
		ImageRegistryPrefix: defaultImportedClusterImageRegistryPrefix,
		ExposureMode:        "nodeport",
	}
	if input.Kubernetes != nil {
		cluster.KubeconfigPath = input.Kubernetes.KubeconfigPath
		cluster.KubeContext = input.Kubernetes.KubeContext
		cluster.ImageRegistryPrefix = firstNonEmpty(input.Kubernetes.ImageRegistryPrefix, cluster.ImageRegistryPrefix)
		cluster.ExposureMode = firstNonEmpty(input.Kubernetes.ExposureMode, cluster.ExposureMode)
		cluster.NodeHost = input.Kubernetes.NodeHost
		cluster.PreviewDomain = input.Kubernetes.PreviewDomain
		cluster.IngressClass = input.Kubernetes.IngressClass
	}
	return cluster
}

func environmentFromCluster(cluster Cluster) Environment {
	return Environment{
		ID:                    cluster.ID,
		WorkspaceID:           cluster.WorkspaceID,
		Name:                  cluster.Name,
		Kind:                  environmentKindKubernetes,
		Status:                firstNonEmpty(cluster.Status, "configured"),
		ProjectCount:          cluster.ProjectCount,
		IssueEnvironmentCount: cluster.EnvironmentCount,
		Kubernetes: &KubernetesEnvironmentConfig{
			ClusterID:           cluster.ID,
			KubeconfigPath:      cluster.KubeconfigPath,
			KubeContext:         cluster.KubeContext,
			ImageRegistryPrefix: cluster.ImageRegistryPrefix,
			ExposureMode:        cluster.ExposureMode,
			NodeHost:            cluster.NodeHost,
			PreviewDomain:       cluster.PreviewDomain,
			IngressClass:        cluster.IngressClass,
		},
		LastCheckedAt: cluster.LastCheckedAt,
		CreatedAt:     cluster.CreatedAt,
		UpdatedAt:     cluster.UpdatedAt,
	}
}

func scanVirtualMachineEnvironment(row scanner) (Environment, error) {
	var environment Environment
	var vm VirtualMachineEnvironmentConfig
	var labels []byte
	var lastCheckedAt sql.NullTime
	var createdAt, updatedAt time.Time
	if err := row.Scan(
		&environment.ID,
		&environment.WorkspaceID,
		&environment.Name,
		&environment.Kind,
		&environment.Status,
		&vm.SSHHost,
		&vm.SSHPort,
		&vm.SSHUser,
		&vm.SSHAuthRef,
		&vm.Workdir,
		&vm.ServiceHint,
		&labels,
		&lastCheckedAt,
		&createdAt,
		&updatedAt,
		&environment.ProjectCount,
		&environment.IssueEnvironmentCount,
		&environment.TestPlanCount,
		&environment.TestRunCount,
	); err != nil {
		return Environment{}, err
	}
	vm.Labels = copyRawMessage(json.RawMessage(labels))
	if len(vm.Labels) == 0 {
		vm.Labels = json.RawMessage(`{}`)
	}
	environment.VirtualMachine = &vm
	if lastCheckedAt.Valid {
		environment.LastCheckedAt = lastCheckedAt.Time.UTC().Format(time.RFC3339)
	}
	environment.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	environment.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return environment, nil
}

func jsonOrObject(payload json.RawMessage) string {
	normalized, err := normalizeJSONObjectPayload(payload)
	if err != nil {
		return "{}"
	}
	return string(normalized)
}

func environmentSnapshot(environment Environment) json.RawMessage {
	payload, err := json.Marshal(environment)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return copyRawMessage(payload)
}
