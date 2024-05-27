package istio

import (
	"context"

	"github.com/go-logr/logr"
	iopv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/env"
	istiov1alpha1 "maistra.io/istio-operator/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	maistrav1 "github.com/kuadrant/kuadrant-operator/api/external/maistra/v1"
	maistrav2 "github.com/kuadrant/kuadrant-operator/api/external/maistra/v2"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
)

const (
	// (Sail) The istio CR must be named default to process GW API resources
	istioCRName = "default"
)

func controlPlaneConfigMapName() string {
	return env.GetString("ISTIOCONFIGMAP_NAME", "istio")
}

func controlPlaneProviderNamespace() string {
	return env.GetString("ISTIOOPERATOR_NAMESPACE", "istio-system")
}

func controlPlaneProviderName() string {
	return env.GetString("ISTIOOPERATOR_NAME", "istiocontrolplane")
}

func UnregisterExternalAuthorizer(ctx context.Context, cl client.Client, kObj *kuadrantv1beta1.Kuadrant) error {
	isIstioInstalled, err := unregisterExternalAuthorizerIstio(ctx, cl, kObj)

	if err == nil && !isIstioInstalled {
		err = unregisterExternalAuthorizerOSSM(ctx, cl, kObj)
	}

	return err
}

func unregisterExternalAuthorizerIstio(ctx context.Context, cl client.Client, kObj *kuadrantv1beta1.Kuadrant) (bool, error) {
	logger, _ := logr.FromContext(ctx)
	configsToUpdate, err := getIstioConfigObjects(ctx, cl)
	isIstioInstalled := configsToUpdate != nil

	if !isIstioInstalled || err != nil {
		return isIstioInstalled, err
	}

	kuadrantAuthorizer := newKuadrantAuthorizer(kObj.Namespace)

	for _, config := range configsToUpdate {
		hasAuthorizer, err := hasKuadrantAuthorizer(config, *kuadrantAuthorizer)
		if err != nil {
			return true, err
		}
		if hasAuthorizer {
			if err = UnregisterKuadrantAuthorizer(config, kuadrantAuthorizer); err != nil {
				return true, err
			}

			logger.Info("remove external authorizer from istio meshconfig")
			if err = cl.Update(ctx, config.GetConfigObject()); err != nil {
				return true, err
			}
		}
	}
	return true, nil
}

func unregisterExternalAuthorizerOSSM(ctx context.Context, cl client.Client, kObj *kuadrantv1beta1.Kuadrant) error {
	logger, _ := logr.FromContext(ctx)

	smcp := &maistrav2.ServiceMeshControlPlane{}

	smcpKey := client.ObjectKey{Name: controlPlaneProviderName(), Namespace: controlPlaneProviderNamespace()}
	if err := cl.Get(ctx, smcpKey, smcp); err != nil {
		logger.V(1).Info("failed to get servicemeshcontrolplane object", "key", smcp, "err", err)
		if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			logger.Info("OSSM installation as GatewayAPI provider not found")
			return nil
		}
		logger.Info("==================== unexpected err", "isnomatcherror", meta.IsNoMatchError(err))
		return err
	}

	smcpWrapper := newOSSMControlPlaneWrapper(smcp)
	kuadrantAuthorizer := newKuadrantAuthorizer(kObj.Namespace)

	hasAuthorizer, err := hasKuadrantAuthorizer(smcpWrapper, *kuadrantAuthorizer)
	if err != nil {
		return err
	}
	if hasAuthorizer {
		err = UnregisterKuadrantAuthorizer(smcpWrapper, kuadrantAuthorizer)
		if err != nil {
			return err
		}
		logger.Info("removing external authorizer from  OSSM meshconfig")
		if err := cl.Update(ctx, smcpWrapper.GetConfigObject()); err != nil {
			return err
		}
	}

	return nil
}

func getIstioConfigObjects(ctx context.Context, cl client.Client) ([]configWrapper, error) {
	logger, _ := logr.FromContext(ctx)
	var configsToUpdate []configWrapper

	iop := &iopv1alpha1.IstioOperator{}
	istKey := client.ObjectKey{Name: controlPlaneProviderName(), Namespace: controlPlaneProviderNamespace()}
	err := cl.Get(ctx, istKey, iop)
	// TODO(eguzki): 🔥 this spaghetti code 🔥
	if err == nil {
		configsToUpdate = append(configsToUpdate, NewOperatorWrapper(iop))
	} else if meta.IsNoMatchError(err) || apierrors.IsNotFound(err) {
		// IstioOperator not existing or not CRD not found, so check for Istio CR instead
		ist := &istiov1alpha1.Istio{}
		istKey := client.ObjectKey{Name: istioCRName}
		if err := cl.Get(ctx, istKey, ist); err != nil {
			logger.V(1).Info("failed to get istio object", "key", istKey, "err", err)
			if meta.IsNoMatchError(err) || apierrors.IsNotFound(err) {
				// return nil and nil if there's no istiooperator or istio CR
				logger.Info("Istio installation as GatewayAPI provider not found")
				return nil, nil
			} else {
				// return nil and err if there's an error other than not found (no istio CR)
				return nil, err
			}
		}
		configsToUpdate = append(configsToUpdate, NewSailWrapper(ist))
	} else {
		logger.V(1).Info("failed to get istiooperator object", "key", istKey, "err", err)
		return nil, err
	}

	istioConfigMap := &corev1.ConfigMap{}
	if err := cl.Get(ctx, client.ObjectKey{Name: controlPlaneConfigMapName(), Namespace: controlPlaneProviderNamespace()}, istioConfigMap); err != nil {
		if !apierrors.IsNotFound(err) {
			logger.V(1).Info("failed to get istio configMap", "key", istKey, "err", err)
			return configsToUpdate, err
		}
	} else {
		configsToUpdate = append(configsToUpdate, NewConfigMapWrapper(istioConfigMap))
	}
	return configsToUpdate, nil
}

func RegisterExternalAuthorizer(ctx context.Context, cl client.Client, kObj *kuadrantv1beta1.Kuadrant) error {
	isIstioInstalled, err := registerExternalAuthorizerIstio(ctx, cl, kObj)

	if err == nil && !isIstioInstalled {
		err = registerExternalAuthorizerOSSM(ctx, cl, kObj)
	}

	return err
}

func registerExternalAuthorizerIstio(ctx context.Context, cl client.Client, kObj *kuadrantv1beta1.Kuadrant) (bool, error) {
	logger, _ := logr.FromContext(ctx)
	configsToUpdate, err := getIstioConfigObjects(ctx, cl)
	isIstioInstalled := configsToUpdate != nil

	if !isIstioInstalled || err != nil {
		return isIstioInstalled, err
	}

	kuadrantAuthorizer := newKuadrantAuthorizer(kObj.Namespace)
	for _, config := range configsToUpdate {
		hasAuthorizer, err := hasKuadrantAuthorizer(config, *kuadrantAuthorizer)
		if err != nil {
			return true, err
		}
		if !hasAuthorizer {
			err = RegisterKuadrantAuthorizer(config, kuadrantAuthorizer)
			if err != nil {
				return true, err
			}
			logger.Info("adding external authorizer to istio meshconfig")
			if err = cl.Update(ctx, config.GetConfigObject()); err != nil {
				return true, err
			}
		}
	}

	return true, nil
}

func registerExternalAuthorizerOSSM(ctx context.Context, cl client.Client, kObj *kuadrantv1beta1.Kuadrant) error {
	logger, _ := logr.FromContext(ctx)

	smcp := &maistrav2.ServiceMeshControlPlane{}

	smcpKey := client.ObjectKey{Name: controlPlaneProviderName(), Namespace: controlPlaneProviderNamespace()}
	if err := cl.Get(ctx, smcpKey, smcp); err != nil {
		logger.V(1).Info("failed to get servicemeshcontrolplane object", "key", smcp, "err", err)
		if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			logger.Info("OSSM installation as GatewayAPI provider not found")
			return nil
		}
		return err
	}

	if err := registerServiceMeshMember(ctx, cl, kObj); err != nil {
		return err
	}

	smcpWrapper := newOSSMControlPlaneWrapper(smcp)
	kuadrantAuthorizer := newKuadrantAuthorizer(kObj.Namespace)

	hasAuthorizer, err := hasKuadrantAuthorizer(smcpWrapper, *kuadrantAuthorizer)
	if err != nil {
		return err
	}
	if !hasAuthorizer {
		err = RegisterKuadrantAuthorizer(smcpWrapper, kuadrantAuthorizer)
		if err != nil {
			return err
		}
		logger.Info("adding external authorizer to OSSM meshconfig")
		if err := cl.Update(ctx, smcpWrapper.GetConfigObject()); err != nil {
			return err
		}
	}

	return nil
}

func registerServiceMeshMember(ctx context.Context, cl client.Client, kObj *kuadrantv1beta1.Kuadrant) error {
	logger, _ := logr.FromContext(ctx)

	member := &maistrav1.ServiceMeshMember{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ServiceMeshMember",
			APIVersion: maistrav1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default",
			Namespace: kObj.Namespace,
		},
		Spec: maistrav1.ServiceMeshMemberSpec{
			ControlPlaneRef: maistrav1.ServiceMeshControlPlaneRef{
				Name:      controlPlaneProviderName(),
				Namespace: controlPlaneProviderNamespace(),
			},
		},
	}

	err := controllerutil.SetControllerReference(kObj, member, cl.Scheme())
	if err != nil {
		logger.Error(err, "Error setting OwnerReference on object",
			"Kind", member.GetObjectKind().GroupVersionKind().String(),
			"Namespace", member.GetNamespace(),
			"Name", member.GetName(),
		)
		return err
	}

	// Create only mutator implementation. If does not exist, create it. Period.
	existing := &maistrav1.ServiceMeshMember{}
	if err := cl.Get(ctx, client.ObjectKeyFromObject(member), existing); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}

		// Not found
		err = cl.Create(ctx, member)
		logger.Info("create service mesh member", "key", client.ObjectKeyFromObject(member), "error", err)
		return err
	}

	// object exists
	return nil
}
