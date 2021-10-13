// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package clusterdeployments

import (
	"fmt"
	"time"

	datatypes "github.com/open-cluster-management/hub-of-hubs-data-types"
	configv1 "github.com/open-cluster-management/hub-of-hubs-data-types/apis/config/v1"
	"github.com/open-cluster-management/leaf-hub-status-sync/pkg/bundle"
	"github.com/open-cluster-management/leaf-hub-status-sync/pkg/controller/generic"
	"github.com/open-cluster-management/leaf-hub-status-sync/pkg/helpers"
	"github.com/open-cluster-management/leaf-hub-status-sync/pkg/transport"
	cdv1 "github.com/openshift/hive/apis/hive/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	componentName                     = "clusterdeployments"
	logName                           = "clusterdeployment-status-sync"
	clusterdeploymentCleanupFinalizer = "hub-of-hubs.open-cluster-management.io/clusterdeployment-cleanup"
)

// AddClustersStatusController adds managed clusters status controller to the manager.
func AddClusterDeploymentStatusController(mgr ctrl.Manager, transport transport.Transport, syncInterval time.Duration,
	leafHubName string, hubOfHubsConfig *configv1.Config) error {
	createObjFunction := func() bundle.Object { return &cdv1.ClusterDeployment{} }
	transportBundleKey := fmt.Sprintf("%s.%s", leafHubName, componentName)

	bundleCollection := []*generic.BundleCollectionEntry{ // single bundle for managed clusters
		generic.NewBundleCollectionEntry(transportBundleKey, bundle.NewGenericStatusBundle(leafHubName,
			helpers.GetBundleGenerationFromTransport(transport, transportBundleKey, datatypes.StatusBundle)),
			func() bool { // bundle predicate
				return hubOfHubsConfig.Spec.AggregationLevel == configv1.Full ||
					hubOfHubsConfig.Spec.AggregationLevel == configv1.Minimal
			}), // at this point send all managed clusters even if aggregation level is minimal
	}

	if err := generic.NewGenericStatusSyncController(mgr, logName, transport,
		clusterdeploymentCleanupFinalizer, bundleCollection, createObjFunction, syncInterval, nil); err != nil {
		return fmt.Errorf("failed to add %s controller to the manager - %w", componentName, err)
	}

	return nil
}
