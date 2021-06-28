package bundle

import (
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/types"
	"sync"
)

func NewGenericStatusBundle(leafHubName string) Bundle {
	return &GenericStatusBundle{
		Objects:     make([]Object, 0),
		LeafHubName: leafHubName,
		generation:  0,
		lock:        sync.Mutex{},
	}
}

// GenericStatusBundle is a bundle that is used to send to the hub of hubs the leaf CR as is
// except for fields that are not relevant in the hub of hubs like finalizers, etc.
// for bundles that require more specific behavior, it's required to implement your own status bundle struct.
type GenericStatusBundle struct {
	Objects     []Object `json:"objects"`
	LeafHubName string   `json:"leafHubName"`
	generation  uint64
	lock        sync.Mutex
}

func (bundle *GenericStatusBundle) UpdateObject(object Object) {
	bundle.lock.Lock()
	defer bundle.lock.Unlock()

	index, err := bundle.getObjectIndexByUID(object.GetUID())
	if err != nil { // object not found, need to add it to the bundle
		bundle.Objects = append(bundle.Objects, object)
		bundle.generation++
		return
	}

	// if we reached here, object already exists in the bundle.. check if we need to update the object
	if object.GetResourceVersion() <= bundle.Objects[index].GetResourceVersion() {
		return // update object only if there is a newer version. check for changes using resourceVersion field
	}
	bundle.Objects[index] = object
	bundle.generation++
}

func (bundle *GenericStatusBundle) DeleteObject(object Object) {
	bundle.lock.Lock()
	defer bundle.lock.Unlock()

	index, err := bundle.getObjectIndexByUID(object.GetUID())
	if err != nil { // trying to delete object which doesn't exist - return with no error
		return
	}
	bundle.Objects = append(bundle.Objects[:index], bundle.Objects[index+1:]...) // remove from objects
	bundle.generation++
}

func (bundle *GenericStatusBundle) GetBundleGeneration() uint64 {
	bundle.lock.Lock()
	defer bundle.lock.Unlock()

	return bundle.generation
}

func (bundle *GenericStatusBundle) getObjectIndexByUID(uid types.UID) (int, error) {
	for i, object := range bundle.Objects {
		if object.GetUID() == uid {
			return i, nil
		}
	}
	return -1, errors.New("object not found")
}