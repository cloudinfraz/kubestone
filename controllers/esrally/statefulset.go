/*
Copyright 2019 The xridge kubestone contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package esrally

import (
	"fmt"
	"github.com/xridge/kubestone/api/v1alpha1"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strings"
)

func NewStatefulSet(cr *v1alpha1.EsRally) (*v1.StatefulSet, error) {
	selectorLabels := map[string]string{
		"perf.kubestone.xridge.io/benchmark": "esrally",
		"perf.kubestone.xridge.io/instance":  cr.Name,
	}
	podLabels := map[string]string{}
	coordinatorHostname := fmt.Sprintf("%s-0.%s", cr.Name, cr.Name)

	for k, v := range selectorLabels {
		podLabels[k] = v
	}

	for k, v := range cr.Spec.PodConfig.PodLabels {
		podLabels[k] = v
	}

	quantity, err := resource.ParseQuantity(cr.Spec.Persistence.Size)
	if err != nil {
		return nil, err
	}

	volumeClaims := []corev1.PersistentVolumeClaim{
		corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: "data",
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{
					corev1.ReadWriteOnce,
				},
				Selector: nil,
				Resources: corev1.ResourceRequirements{
					Limits: nil,
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: quantity,
					},
				},
				StorageClassName: &cr.Spec.Persistence.StorageClass,
			},
		},
	}

	replicas := int32(1)
	if cr.Spec.Nodes != nil {
		replicas = *cr.Spec.Nodes
	}

	objectMeta := metav1.ObjectMeta{
		Name:      cr.Name,
		Namespace: cr.Namespace,
	}

	initContainer := createEsRallyContainer(cr, "init", "mkdir -p  /esrally/benchmarks; chown -R rally:rally /esrally")
	initContainer.VolumeMounts = []corev1.VolumeMount{
		corev1.VolumeMount{
			Name:      "data",
			MountPath: "/esrally",
		},
	}
	rootUid := int64(0)
	initContainer.SecurityContext = &corev1.SecurityContext{
		RunAsUser: &rootUid,
	}

	esrallydContainer := createEsRallyContainer(cr, "esrallyd",
		strings.Join([]string{
			"touch /rally/.rally/logs/rally.log;",
			"/usr/local/bin/esrallyd", "start", "--node-ip=${MY_POD_IP}", fmt.Sprintf("--coordinator-ip=%s;", coordinatorHostname),
			fmt.Sprintf("if  [ ${HOSTNAME} != '%s-0' ];then\n", cr.Name),
			"tail -f /rally/.rally/logs/rally.log\n",
			"else\n",
			"tail -f /rally/.rally/logs/rally.log &",
			"echo 'I am coordinator';",
			"sleep 60;",
			strings.Join(CreateEsRallyCmd(&cr.Spec, &objectMeta), " "), ";",
			"fi\n",
			"cat ~/.rally/logs/rally.log | grep PID | sed -r \"s/.*PID\\:([0-9]+).*/PID \\1/g\" | grep PID | awk '{print \"kill -9 \"$2}' | sh;",
			"true;",
		}, " "),
	)
	esrallydContainer.Ports = []corev1.ContainerPort{
		corev1.ContainerPort{
			Name:          "transport",
			ContainerPort: 1900,
			Protocol:      "TCP",
		},
	}
	esrallydContainer.VolumeMounts = []corev1.VolumeMount{
		corev1.VolumeMount{
			Name:      "data",
			MountPath: "/esrally",
		},
	}

	stateFulSet := v1.StatefulSet{
		ObjectMeta: objectMeta,
		Spec: v1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: selectorLabels,
			},
			ServiceName: cr.Name,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      podLabels,
					Annotations: cr.Spec.PodConfig.Annotations,
				},
				Spec: corev1.PodSpec{
					NodeSelector: cr.Spec.PodConfig.PodScheduling.NodeSelector,
					Affinity:     cr.Spec.PodConfig.PodScheduling.Affinity,
					Tolerations:  cr.Spec.PodConfig.PodScheduling.Tolerations,
					InitContainers: []corev1.Container{
						initContainer,
					},
					Containers: []corev1.Container{
						esrallydContainer,
					},
				},
			},
			VolumeClaimTemplates: volumeClaims,
		},
	}

	return &stateFulSet, nil
}

func createEsRallyContainer(cr *v1alpha1.EsRally, name string, command string) corev1.Container {
	return corev1.Container{
		Name:            name,
		Image:           cr.Spec.Image.Name,
		ImagePullPolicy: corev1.PullPolicy(cr.Spec.Image.PullPolicy),
		Resources:       cr.Spec.PodConfig.Resources,
		Env: []corev1.EnvVar{{
			Name: "MY_POD_IP", ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
			}},
		},
		Command: []string{
			"/bin/sh", "-c",
		},
		Args: []string{command},
	}
}
