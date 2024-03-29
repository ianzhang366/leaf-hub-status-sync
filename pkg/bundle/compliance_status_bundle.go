package bundle

import (
	"sync"

	policyv1 "github.com/open-cluster-management/governance-policy-propagator/pkg/apis/policy/v1"
	datatypes "github.com/open-cluster-management/hub-of-hubs-data-types"
	statusbundle "github.com/open-cluster-management/hub-of-hubs-data-types/bundle/status"
	"github.com/open-cluster-management/leaf-hub-status-sync/pkg/helpers"
)

// NewComplianceStatusBundle creates a new instance of ComplianceStatusBundle.
func NewComplianceStatusBundle(leafHubName string, baseBundle Bundle, generation uint64) Bundle {
	return &ComplianceStatusBundle{
		BaseComplianceStatusBundle: statusbundle.BaseComplianceStatusBundle{
			Objects:              make([]*statusbundle.PolicyComplianceStatus, 0),
			LeafHubName:          leafHubName,
			BaseBundleGeneration: baseBundle.GetBundleGeneration(),
			Generation:           generation,
		},
		baseBundle: baseBundle,
		lock:       sync.Mutex{},
	}
}

// ComplianceStatusBundle abstracts management of compliance status bundle.
type ComplianceStatusBundle struct {
	statusbundle.BaseComplianceStatusBundle
	baseBundle Bundle
	lock       sync.Mutex
}

// UpdateObject function to update a single object inside a bundle.
func (bundle *ComplianceStatusBundle) UpdateObject(object Object) {
	bundle.lock.Lock()
	defer bundle.lock.Unlock()

	bundle.BaseBundleGeneration = bundle.baseBundle.GetBundleGeneration()

	policy, ok := object.(*policyv1.Policy)
	if !ok {
		return // do not handle objects other than policy
	}

	originPolicyID, found := object.GetAnnotations()[datatypes.OriginOwnerReferenceAnnotation]
	if !found {
		return // origin owner reference annotation not found, not handling policy that wasn't sent from hub of hubs
	}

	index, err := bundle.getObjectIndexByUID(originPolicyID)
	if err != nil { // object not found, need to add it to the bundle
		policyComplianceObject := bundle.getPolicyComplianceStatus(originPolicyID, policy)
		// don't send in the bundle a policy where all clusters are compliant
		if bundle.containsNonCompliantOrUnknownClusters(policyComplianceObject) {
			bundle.Objects = append(bundle.Objects, policyComplianceObject)
		}
		bundle.Generation++

		return
	}

	// if we reached here, object already exists in the bundle with at least one non compliant or unknown cluster.
	if object.GetResourceVersion() == bundle.Objects[index].ResourceVersion { // check if the object has changed.
		return // update in bundle only if object changed. check for changes using resourceVersion field
	}

	if !bundle.updateBundleIfObjectChanged(index, policy) {
		return // true if changed,otherwise false. if policy compliance didn't change don't increment generation.
	}

	// don't send in the bundle a policy where all clusters are compliant
	if !bundle.containsNonCompliantOrUnknownClusters(bundle.Objects[index]) {
		bundle.Objects = append(bundle.Objects[:index], bundle.Objects[index+1:]...) // remove from objects
	} else { // we have at least one cluster non compliant or unknown and cluster list has changed
		bundle.Objects[index].ResourceVersion = object.GetResourceVersion() // update resource version of the object
	}

	// increase bundle generation in the case where cluster lists were changed
	bundle.Generation++
}

// DeleteObject function to delete a single object inside a bundle.
func (bundle *ComplianceStatusBundle) DeleteObject(object Object) {
	bundle.lock.Lock()
	defer bundle.lock.Unlock()

	bundle.BaseBundleGeneration = bundle.baseBundle.GetBundleGeneration()

	originPolicyID, found := object.GetAnnotations()[datatypes.OriginOwnerReferenceAnnotation]
	if !found {
		return // origin owner reference annotation not found, cannot handle this policy
	}

	index, err := bundle.getObjectIndexByUID(originPolicyID)
	if err != nil { // trying to delete object which doesn't exist - return with no error
		return
	}

	// do not increase generation, no need to send bundle when policy is removed (clusters per policy bundle is sent).
	bundle.Objects = append(bundle.Objects[:index], bundle.Objects[index+1:]...) // remove from objects
}

// GetBundleGeneration function to get bundle generation.
func (bundle *ComplianceStatusBundle) GetBundleGeneration() uint64 {
	bundle.lock.Lock()
	defer bundle.lock.Unlock()

	return bundle.Generation
}

func (bundle *ComplianceStatusBundle) getObjectIndexByUID(uid string) (int, error) {
	for i, object := range bundle.Objects {
		if object.PolicyID == uid {
			return i, nil
		}
	}

	return -1, errObjectNotFound
}

func (bundle *ComplianceStatusBundle) getPolicyComplianceStatus(originPolicyID string,
	policy *policyv1.Policy) *statusbundle.PolicyComplianceStatus {
	nonCompliantClusters, unknownComplianceClusters := bundle.getNonCompliantAndUnknownClusters(policy)

	return &statusbundle.PolicyComplianceStatus{
		PolicyID:                  originPolicyID,
		NonCompliantClusters:      nonCompliantClusters,
		UnknownComplianceClusters: unknownComplianceClusters,
		ResourceVersion:           policy.GetResourceVersion(),
	}
}

// returns a list of non compliant clusters and a list of unknown compliance clusters.
func (bundle *ComplianceStatusBundle) getNonCompliantAndUnknownClusters(policy *policyv1.Policy) ([]string, []string) {
	nonCompliantClusters := make([]string, 0)
	unknownComplianceClusters := make([]string, 0)

	for _, clusterCompliance := range policy.Status.Status {
		if clusterCompliance.ComplianceState == policyv1.Compliant {
			continue
		}

		if clusterCompliance.ComplianceState == policyv1.NonCompliant {
			nonCompliantClusters = append(nonCompliantClusters, clusterCompliance.ClusterName)
		} else { // not compliant not non compliant -> means unknown
			unknownComplianceClusters = append(unknownComplianceClusters, clusterCompliance.ClusterName)
		}
	}

	return nonCompliantClusters, unknownComplianceClusters
}

// if a cluster was removed, object is not considered as changed.
func (bundle *ComplianceStatusBundle) updateBundleIfObjectChanged(objectIndex int, policy *policyv1.Policy) bool {
	oldPolicyComplianceStatus := bundle.Objects[objectIndex]
	newNonCompliantClusters, newUnknownComplianceClusters := bundle.getNonCompliantAndUnknownClusters(policy)
	// comparing length, if not equal there is at least one cluster that it's compliance status was changed.
	if len(oldPolicyComplianceStatus.NonCompliantClusters) != len(newNonCompliantClusters) ||
		len(oldPolicyComplianceStatus.UnknownComplianceClusters) != len(newUnknownComplianceClusters) {
		bundle.Objects[objectIndex].NonCompliantClusters = newNonCompliantClusters
		bundle.Objects[objectIndex].UnknownComplianceClusters = newUnknownComplianceClusters

		return true
	}
	// otherwise the length of the lists are equals (old and new lists per type).
	// check each cluster in the new list (could be that new cluster isn't in the list and old one was added and
	// therefore length is equal
	for _, nonCompliantCluster := range newNonCompliantClusters {
		if !helpers.ContainsString(oldPolicyComplianceStatus.NonCompliantClusters, nonCompliantCluster) {
			bundle.Objects[objectIndex].NonCompliantClusters = newNonCompliantClusters
			bundle.Objects[objectIndex].UnknownComplianceClusters = newUnknownComplianceClusters

			return true
		}
	}

	for _, unknownComplianceCluster := range newUnknownComplianceClusters {
		if !helpers.ContainsString(oldPolicyComplianceStatus.UnknownComplianceClusters, unknownComplianceCluster) {
			bundle.Objects[objectIndex].NonCompliantClusters = newNonCompliantClusters
			bundle.Objects[objectIndex].UnknownComplianceClusters = newUnknownComplianceClusters

			return true
		}
	}
	// if we finished the two for loops, all non compliant and unknown can be found inside the existing lists.
	return false
}

func (bundle *ComplianceStatusBundle) containsNonCompliantOrUnknownClusters(
	policyComplianceStatus *statusbundle.PolicyComplianceStatus) bool {
	if len(policyComplianceStatus.UnknownComplianceClusters) == 0 &&
		len(policyComplianceStatus.NonCompliantClusters) == 0 {
		return false
	}

	return true
}
