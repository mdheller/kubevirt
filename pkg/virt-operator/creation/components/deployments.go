/*
 * This file is part of the KubeVirt project
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 * Copyright 2018 Red Hat, Inc.
 *
 */
package components

import (
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/api/policy/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/json"

	virtv1 "kubevirt.io/client-go/api/v1"
	"kubevirt.io/kubevirt/pkg/virt-operator/creation/rbac"
	operatorutil "kubevirt.io/kubevirt/pkg/virt-operator/util"
)

func NewPrometheusService(namespace string) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "kubevirt-prometheus-metrics",
			Labels: map[string]string{
				virtv1.AppLabel:          "",
				"prometheus.kubevirt.io": "",
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"prometheus.kubevirt.io": "",
			},
			Ports: []corev1.ServicePort{
				{
					Name: "metrics",
					Port: 443,
					TargetPort: intstr.IntOrString{
						Type:   intstr.String,
						StrVal: "metrics",
					},
					Protocol: corev1.ProtocolTCP,
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}
}

func NewApiServerService(namespace string) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "virt-api",
			Labels: map[string]string{
				virtv1.AppLabel: "virt-api",
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				virtv1.AppLabel: "virt-api",
			},
			Ports: []corev1.ServicePort{
				{
					Port: 443,
					TargetPort: intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 8443,
					},
					Protocol: corev1.ProtocolTCP,
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}
}

func newPodTemplateSpec(podName string, imageName string, repository string, version string, pullPolicy corev1.PullPolicy, podAffinity *corev1.Affinity) (*corev1.PodTemplateSpec, error) {

	version = AddVersionSeparatorPrefix(version)

	tolerations, err := criticalAddonsToleration()
	if err != nil {
		return nil, fmt.Errorf("unable to create toleration: %v", err)
	}

	return &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				virtv1.AppLabel:          podName,
				"prometheus.kubevirt.io": "",
			},
			Annotations: map[string]string{
				"scheduler.alpha.kubernetes.io/critical-pod": "",
				"scheduler.alpha.kubernetes.io/tolerations":  string(tolerations),
			},
			Name: podName,
		},
		Spec: corev1.PodSpec{
			Affinity: podAffinity,
			Containers: []corev1.Container{
				{
					Name:            podName,
					Image:           fmt.Sprintf("%s/%s%s", repository, imageName, version),
					ImagePullPolicy: pullPolicy,
				},
			},
		},
	}, nil
}

func newBaseDeployment(deploymentName string, imageName string, namespace string, repository string, version string, pullPolicy corev1.PullPolicy, podAffinity *corev1.Affinity) (*appsv1.Deployment, error) {

	podTemplateSpec, err := newPodTemplateSpec(deploymentName, imageName, repository, version, pullPolicy, podAffinity)
	if err != nil {
		return nil, err
	}

	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      deploymentName,
			Labels: map[string]string{
				virtv1.AppLabel: deploymentName,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(2),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"kubevirt.io": deploymentName,
				},
			},
			Template: *podTemplateSpec,
		},
	}, nil
}

func newPodAntiAffinity(key, topologyKey string, operator metav1.LabelSelectorOperator, values []string) *corev1.Affinity {
	return &corev1.Affinity{
		PodAntiAffinity: &corev1.PodAntiAffinity{
			PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
				{
					Weight: 1,
					PodAffinityTerm: corev1.PodAffinityTerm{
						LabelSelector: &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      key,
									Operator: operator,
									Values:   values,
								},
							},
						},
						TopologyKey: topologyKey,
					},
				},
			},
		},
	}
}

func NewApiServerDeployment(namespace string, repository string, imagePrefix string, version string, pullPolicy corev1.PullPolicy, verbosity string) (*appsv1.Deployment, error) {
	podAntiAffinity := newPodAntiAffinity("kubevirt.io", "kubernetes.io/hostname", metav1.LabelSelectorOpIn, []string{"virt-api"})
	deploymentName := "virt-api"
	imageName := fmt.Sprintf("%s%s", imagePrefix, deploymentName)
	deployment, err := newBaseDeployment(deploymentName, imageName, namespace, repository, version, pullPolicy, podAntiAffinity)
	if err != nil {
		return nil, err
	}

	pod := &deployment.Spec.Template.Spec
	pod.ServiceAccountName = rbac.ApiServiceAccountName
	pod.SecurityContext = &corev1.PodSecurityContext{
		RunAsNonRoot: boolPtr(true),
	}

	container := &deployment.Spec.Template.Spec.Containers[0]
	container.Command = []string{
		"virt-api",
		"--port",
		"8443",
		"--console-server-port",
		"8186",
		"--subresources-only",
		"-v",
		verbosity,
	}
	container.Ports = []corev1.ContainerPort{
		{
			Name:          "virt-api",
			Protocol:      corev1.ProtocolTCP,
			ContainerPort: 8443,
		},
		{
			Name:          "metrics",
			Protocol:      corev1.ProtocolTCP,
			ContainerPort: 8443,
		},
	}
	container.ReadinessProbe = &corev1.Probe{
		Handler: corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Scheme: corev1.URISchemeHTTPS,
				Port: intstr.IntOrString{
					Type:   intstr.Int,
					IntVal: 8443,
				},
				Path: "/apis/subresources.kubevirt.io/" + virtv1.SubresourceGroupVersions[0].Version + "/healthz",
			},
		},
		InitialDelaySeconds: 15,
		PeriodSeconds:       10,
	}
	return deployment, nil
}

func NewControllerDeployment(namespace string, repository string, imagePrefix string, controllerVersion string, launcherVersion string, pullPolicy corev1.PullPolicy, verbosity string) (*appsv1.Deployment, error) {
	podAntiAffinity := newPodAntiAffinity("kubevirt.io", "kubernetes.io/hostname", metav1.LabelSelectorOpIn, []string{"virt-controller"})
	deploymentName := "virt-controller"
	imageName := fmt.Sprintf("%s%s", imagePrefix, deploymentName)
	deployment, err := newBaseDeployment(deploymentName, imageName, namespace, repository, controllerVersion, pullPolicy, podAntiAffinity)
	if err != nil {
		return nil, err
	}

	pod := &deployment.Spec.Template.Spec
	pod.ServiceAccountName = rbac.ControllerServiceAccountName
	pod.SecurityContext = &corev1.PodSecurityContext{
		RunAsNonRoot: boolPtr(true),
	}

	launcherVersion = AddVersionSeparatorPrefix(launcherVersion)

	container := &deployment.Spec.Template.Spec.Containers[0]
	container.Command = []string{
		"virt-controller",
		"--launcher-image",
		fmt.Sprintf("%s/%s%s%s", repository, imagePrefix, "virt-launcher", launcherVersion),
		"--port",
		"8443",
		"-v",
		verbosity,
	}
	container.Ports = []corev1.ContainerPort{
		{
			Name:          "metrics",
			Protocol:      corev1.ProtocolTCP,
			ContainerPort: 8443,
		},
	}
	container.LivenessProbe = &corev1.Probe{
		FailureThreshold: 8,
		Handler: corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Scheme: corev1.URISchemeHTTPS,
				Port: intstr.IntOrString{
					Type:   intstr.Int,
					IntVal: 8443,
				},
				Path: "/healthz",
			},
		},
		InitialDelaySeconds: 15,
		TimeoutSeconds:      10,
	}
	container.ReadinessProbe = &corev1.Probe{
		Handler: corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Scheme: corev1.URISchemeHTTPS,
				Port: intstr.IntOrString{
					Type:   intstr.Int,
					IntVal: 8443,
				},
				Path: "/leader",
			},
		},
		InitialDelaySeconds: 15,
		TimeoutSeconds:      10,
	}
	return deployment, nil
}

func NewHandlerDaemonSet(namespace string, repository string, imagePrefix string, version string, pullPolicy corev1.PullPolicy, verbosity string) (*appsv1.DaemonSet, error) {

	deploymentName := "virt-handler"
	imageName := fmt.Sprintf("%s%s", imagePrefix, deploymentName)
	podTemplateSpec, err := newPodTemplateSpec(deploymentName, imageName, repository, version, pullPolicy, nil)
	if err != nil {
		return nil, err
	}

	daemonset := &appsv1.DaemonSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "DaemonSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "virt-handler",
			Labels: map[string]string{
				virtv1.AppLabel: "virt-handler",
			},
		},
		Spec: appsv1.DaemonSetSpec{
			UpdateStrategy: appsv1.DaemonSetUpdateStrategy{
				Type: appsv1.RollingUpdateDaemonSetStrategyType,
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"kubevirt.io": "virt-handler",
				},
			},
			Template: *podTemplateSpec,
		},
	}

	pod := &daemonset.Spec.Template.Spec
	pod.ServiceAccountName = rbac.HandlerServiceAccountName
	pod.HostPID = true

	container := &pod.Containers[0]
	container.Command = []string{
		"virt-handler",
		"--port",
		"8443",
		"--hostname-override",
		"$(NODE_NAME)",
		"--pod-ip-address",
		"$(MY_POD_IP)",
		"--max-metric-requests",
		"3",
		"--console-server-port",
		"8186",
		"-v",
		verbosity,
	}
	container.Ports = []corev1.ContainerPort{
		{
			Name:          "metrics",
			Protocol:      corev1.ProtocolTCP,
			ContainerPort: 8443,
		},
	}
	container.SecurityContext = &corev1.SecurityContext{
		Privileged: boolPtr(true),
	}
	container.Env = []corev1.EnvVar{
		{
			Name: "NODE_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "spec.nodeName",
				},
			},
		},
		{
			Name: "MY_POD_IP",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "status.podIP",
				},
			},
		},
	}

	container.VolumeMounts = []corev1.VolumeMount{}
	pod.Volumes = []corev1.Volume{}

	type volume struct {
		name             string
		path             string
		mountPropagation *corev1.MountPropagationMode
	}

	bidi := corev1.MountPropagationBidirectional
	volumes := []volume{
		{"libvirt-runtimes", "/var/run/kubevirt-libvirt-runtimes", nil},
		{"virt-share-dir", "/var/run/kubevirt", &bidi},
		{"virt-lib-dir", "/var/lib/kubevirt", nil},
		{"virt-private-dir", "/var/run/kubevirt-private", nil},
		{"device-plugin", "/var/lib/kubelet/device-plugins", nil},
	}

	for _, volume := range volumes {
		container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
			Name:             volume.name,
			MountPath:        volume.path,
			MountPropagation: volume.mountPropagation,
		})
		pod.Volumes = append(pod.Volumes, corev1.Volume{
			Name: volume.name,
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: volume.path,
				},
			},
		})
	}

	return daemonset, nil

}

// Used for manifest generation only
func NewOperatorDeployment(namespace string, repository string, imagePrefix string, version string,
	pullPolicy corev1.PullPolicy, verbosity string,
	kubeVirtVersionEnv string, virtApiShaEnv string, virtControllerShaEnv string,
	virtHandlerShaEnv string, virtLauncherShaEnv string) (*appsv1.Deployment, error) {

	podAntiAffinity := newPodAntiAffinity("kubevirt.io", "kubernetes.io/hostname", metav1.LabelSelectorOpIn, []string{"virt-operator"})
	name := "virt-operator"
	version = AddVersionSeparatorPrefix(version)
	image := fmt.Sprintf("%s/%s%s%s", repository, imagePrefix, name, version)

	tolerations, err := criticalAddonsToleration()
	if err != nil {
		return nil, fmt.Errorf("unable to create toleration: %v", err)
	}

	deployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels: map[string]string{
				virtv1.AppLabel: name,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(2),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					virtv1.AppLabel: name,
				},
			},
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						virtv1.AppLabel:          name,
						"prometheus.kubevirt.io": "",
					},
					Annotations: map[string]string{
						"scheduler.alpha.kubernetes.io/critical-pod": "",
						"scheduler.alpha.kubernetes.io/tolerations":  string(tolerations),
					},
					Name: name,
				},
				Spec: corev1.PodSpec{
					Affinity:           podAntiAffinity,
					ServiceAccountName: "kubevirt-operator",
					Containers: []corev1.Container{
						{
							Name:            name,
							Image:           image,
							ImagePullPolicy: pullPolicy,
							Command: []string{
								"virt-operator",
								"--port",
								"8443",
								"-v",
								verbosity,
							},
							Ports: []corev1.ContainerPort{
								{
									Name:          "metrics",
									Protocol:      corev1.ProtocolTCP,
									ContainerPort: 8443,
								},
								{
									Name:          "webhooks",
									Protocol:      corev1.ProtocolTCP,
									ContainerPort: 8444,
								},
							},
							ReadinessProbe: &corev1.Probe{
								Handler: corev1.Handler{
									HTTPGet: &corev1.HTTPGetAction{
										Scheme: corev1.URISchemeHTTPS,
										Port: intstr.IntOrString{
											Type:   intstr.Int,
											IntVal: 8443,
										},
										Path: "/metrics",
									},
								},
								InitialDelaySeconds: 5,
								TimeoutSeconds:      10,
							},
							Env: []corev1.EnvVar{
								{
									Name:  operatorutil.OperatorImageEnvName,
									Value: image,
								},
								{
									Name: "WATCH_NAMESPACE", // not used yet
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "metadata.annotations['olm.targetNamespaces']", // filled by OLM
										},
									},
								},
							},
						},
					},
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: boolPtr(true),
					},
				},
			},
		},
	}

	if virtApiShaEnv != "" && virtControllerShaEnv != "" && virtHandlerShaEnv != "" && virtLauncherShaEnv != "" && kubeVirtVersionEnv != "" {
		shaSums := []corev1.EnvVar{
			{
				Name:  operatorutil.KubeVirtVersionEnvName,
				Value: kubeVirtVersionEnv,
			},
			{
				Name:  operatorutil.VirtApiShasumEnvName,
				Value: virtApiShaEnv,
			},
			{
				Name:  operatorutil.VirtControllerShasumEnvName,
				Value: virtControllerShaEnv,
			},
			{
				Name:  operatorutil.VirtHandlerShasumEnvName,
				Value: virtHandlerShaEnv,
			},
			{
				Name:  operatorutil.VirtLauncherShasumEnvName,
				Value: virtLauncherShaEnv,
			},
		}
		env := deployment.Spec.Template.Spec.Containers[0].Env
		env = append(env, shaSums...)
		deployment.Spec.Template.Spec.Containers[0].Env = env
	}

	return deployment, nil
}

func int32Ptr(i int32) *int32 {
	return &i
}

func boolPtr(b bool) *bool {
	return &b
}

func criticalAddonsToleration() ([]byte, error) {
	tolerations := []corev1.Toleration{
		{
			Key:      "CriticalAddonsOnly",
			Operator: corev1.TolerationOpExists,
		},
	}
	tolerationsStr, err := json.Marshal(tolerations)
	return tolerationsStr, err
}

func AddVersionSeparatorPrefix(version string) string {
	// version can be a template, a tag or shasum
	// prefix tags with ":" and shasums with "@"
	// templates have to deal with the correct image/version separator themselves
	if strings.HasPrefix(version, "sha256:") {
		version = fmt.Sprintf("@%s", version)
	} else if !strings.HasPrefix(version, "{{if") {
		version = fmt.Sprintf(":%s", version)
	}
	return version
}

func NewPodDisruptionBudgetForDeployment(deployment *appsv1.Deployment) *v1beta1.PodDisruptionBudget {
	pdbName := deployment.Name + "-pdb"
	minAvailable := intstr.FromInt(int(1))
	selector := deployment.Spec.Selector.DeepCopy()
	podDisruptionBudget := &v1beta1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: deployment.Namespace,
			Name:      pdbName,
			Labels: map[string]string{
				virtv1.AppLabel: pdbName,
			},
		},
		Spec: v1beta1.PodDisruptionBudgetSpec{
			MinAvailable: &minAvailable,
			Selector:     selector,
		},
	}
	return podDisruptionBudget
}
