// Copyright 2016-2018, Pulumi Corporation.  All rights reserved.

package deploy

import (
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/pkg/errors"

	"github.com/pulumi/pulumi/pkg/resource"
	"github.com/pulumi/pulumi/pkg/tokens"
	"github.com/pulumi/pulumi/pkg/util/contract"
	"github.com/pulumi/pulumi/pkg/workspace"
)

// Snapshot is a view of a collection of resources in an stack at a point in time.  It describes resources; their
// IDs, names, and properties; their dependencies; and more.  A snapshot is a diffable entity and can be used to create
// or apply an infrastructure deployment plan in order to make reality match the snapshot state.
type Snapshot struct {
	Stack     tokens.QName      // the stack being deployed into.
	Manifest  Manifest          // a deployment manifest of versions, checksums, and so on.
	Resources []*resource.State // fetches all resources and their associated states.
}

// Manifest captures versions for all binaries used to construct this snapshot.
type Manifest struct {
	Time    time.Time              // the time this snapshot was taken.
	Magic   string                 // a magic cookie.
	Version string                 // the pulumi command version.
	Plugins []workspace.PluginInfo // the plugin versions also loaded.
}

// NewMagic creates a magic cookie out of a manifest; this can be used to check for tampering.  This ignores
// any existing magic value already stored on the manifest.
func (m Manifest) NewMagic() string {
	if m.Version == "" {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(m.Version)))
}

// NewSnapshot creates a snapshot from the given arguments.  The resources must be in topologically sorted order.
// This property is not checked; for verification, please refer to the VerifyIntegrity function below.
func NewSnapshot(stack tokens.QName, manifest Manifest, resources []*resource.State) *Snapshot {
	return &Snapshot{
		Stack:     stack,
		Manifest:  manifest,
		Resources: resources,
	}
}

// VerifyIntegrity checks a snapshot to ensure it is well-formed.  Because of the cost of this operation,
// integrity verification is only performed on demand, and not automatically during snapshot construction.
//
// This function enforces a couple of invariants:
//  1. Parents should always come before children in the resource list
//  2. Dependents should always come before their dependencies in the resource list
//  3. For every URN in the snapshot, there must be at most one resource in a "live" state.
//  4. The magic manifest number should change every time the snapshot is mutated
//
// A state is "live" if it represents a resource that has been created or updated successfully and exists.
func (snap *Snapshot) VerifyIntegrity() error {
	contract.Require(snap != nil, "snap != nil")

	// Ensure the magic cookie checks out.
	if snap.Manifest.Magic != snap.Manifest.NewMagic() {
		return errors.Errorf("magic cookie mismatch; possible tampering/corruption detected")
	}

	// Now check the resources.  For now, we just verify that parents come before children, and that there aren't
	// any duplicate URNs.  Eventually, we will capture the full resource DAG (see
	// https://github.com/pulumi/pulumi/issues/624), on which we can then do additional verification.
	urns := make(map[resource.URN]*resource.State)
	seenLive := make(map[resource.URN]bool)
	for i, state := range snap.Resources {
		urn := state.URN
		if par := state.Parent; par != "" {
			if _, has := urns[par]; !has {
				// The parent isn't there; to give a good error message, see whether it's missing entirely, or
				// whether it comes later in the snapshot (neither of which should ever happen).
				for _, other := range snap.Resources[i+1:] {
					if other.URN == par {
						return errors.Errorf("child resource %s's parent %s comes after it", urn, par)
					}
				}
				return errors.Errorf("child resource %s refers to missing parent %s", urn, par)
			}
		}

		for _, dep := range state.Dependencies {
			if _, has := urns[dep]; !has {
				// same as above - doing this for better error messages
				for _, other := range snap.Resources[i+1:] {
					if other.URN == dep {
						return errors.Errorf("resource %s's dependency %s comes after it", urn, other.URN)
					}
				}

				return errors.Errorf("resource %s dependency %s refers to missing resource", urn, dep)
			}
		}

		if state.Status == resource.ResourceStatusUnspecified {
			return errors.Errorf("resource %s has status `unspecified`", urn)
		}

		if state.Status == resource.ResourceStatusDeleted {
			return errors.Errorf("resource %s has status `deleted`", urn)
		}

		if seen, has := seenLive[urn]; has && seen && state.Status.Live() {
			// There should never be two resources live with the same URN.
			return errors.Errorf("duplicate live resource %s, with status %s", urn, state.Status)
		}

		urns[urn] = state
		if _, has := seenLive[urn]; !has {
			seenLive[urn] = state.Status.Live()
		} else {
			seenLive[urn] = seenLive[urn] || state.Status.Live()
		}
	}

	return nil
}

func (snap *Snapshot) Clone() *Snapshot {
	var newSnap Snapshot
	newSnap.Stack = snap.Stack
	newSnap.Resources = []*resource.State{}
	for _, res := range snap.Resources {
		newSnap.Resources = append(newSnap.Resources, res.Clone())
	}

	return &newSnap
}
