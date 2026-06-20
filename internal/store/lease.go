// Copyright 2026 Simone Vellei
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// Lease is the value stored under packtrail-leases/<execID>.
type Lease struct {
	Owner   string    `json:"owner"`
	Expires time.Time `json:"expires"`
}

func (l Lease) expired() bool { return time.Now().After(l.Expires) }

// AcquireLease attempts to take or renew ownership of execID for owner with the
// given TTL. It succeeds if the key is absent, already owned by owner, or held
// by an expired lease. The CAS guarantees that at most one *distinct* owner wins
// a race; a write that loses to our own concurrent renewal still counts as held.
// It returns true if the lease is now held by owner.
//
//nolint:gocognit,funlen
func (s *Store) AcquireLease(ctx context.Context, execID, owner string, ttl time.Duration) (bool, error) {
	val, err := json.Marshal(Lease{Owner: owner, Expires: time.Now().Add(ttl).UTC()})
	if err != nil {
		return false, err
	}

	// Retry to resolve races (our own heartbeat renewing, or an expired-lease
	// takeover contended by multiple instances).
	for range 8 {
		entry, getErr := s.leases.Get(ctx, execID)
		if errors.Is(getErr, jetstream.ErrKeyNotFound) {
			if _, createErr := s.leases.Create(ctx, execID, val); createErr != nil {
				if errors.Is(createErr, jetstream.ErrKeyExists) {
					continue // someone created it first; re-read
				}

				return false, createErr
			}

			return true, nil
		}

		if getErr != nil {
			return false, getErr
		}

		var cur Lease
		if unmarshalErr := json.Unmarshal(entry.Value(), &cur); unmarshalErr != nil {
			return false, unmarshalErr
		}

		if cur.Owner != owner && !cur.expired() {
			return false, nil // held by someone else
		}
		// We own it, or it expired: take/renew via CAS at the observed revision.
		if _, updateErr := s.leases.Update(ctx, execID, val, entry.Revision()); updateErr != nil {
			if errors.Is(updateErr, jetstream.ErrKeyExists) || isWrongLastSeq(updateErr) {
				if cur.Owner == owner {
					return true, nil // our own renewal won the race; we still own it
				}

				continue // expired-takeover race: re-read to see who won
			}

			return false, updateErr
		}

		return true, nil
	}

	return false, nil
}

// ReleaseLease drops ownership of execID if held by owner. Releasing a lease not
// owned by owner is a no-op.
func (s *Store) ReleaseLease(ctx context.Context, execID, owner string) error {
	entry, err := s.leases.Get(ctx, execID)
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return nil
	}

	if err != nil {
		return err
	}

	var cur Lease

	err = json.Unmarshal(entry.Value(), &cur)
	if err != nil {
		return err
	}

	if cur.Owner != owner {
		return nil
	}

	err = s.leases.Delete(ctx, execID, jetstream.LastRevision(entry.Revision()))
	if errors.Is(err, jetstream.ErrKeyExists) || isWrongLastSeq(err) {
		return nil // someone else took over; leave it alone
	}

	return err
}
