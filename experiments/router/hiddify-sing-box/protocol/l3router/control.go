package l3routerendpoint

import rt "github.com/sagernet/sing-box/experimental/l3router"

func (e *Endpoint) applyRoute(r rt.Route, countControl bool) error {
	if err := ValidateRoute(r); err != nil {
		if countControl {
			e.controlErrors.Add(1)
		}
		return err
	}
	e.sessMu.Lock()
	prevOwner := e.routeOwners[r.ID]
	e.sessMu.Unlock()

	e.engine.UpsertRoute(r)
	if countControl {
		e.controlUpsertOK.Add(1)
	} else {
		e.staticLoadOK.Add(1)
	}
	if prevOwner != "" && prevOwner != r.Owner {
		// Drop only stale bindings for this route when ownership changes.
		e.engine.ClearIngressSessionRoute(r.ID, rt.SessionKey(prevOwner))
		e.engine.ClearEgressSession(r.ID)
	}
	e.sessMu.Lock()
	e.routeOwners[r.ID] = r.Owner
	e.sessMu.Unlock()
	e.refMu.Lock()
	defer e.refMu.Unlock()
	for sk, refs := range e.userRef {
		if refs <= 0 {
			continue
		}
		if string(sk) == r.Owner {
			e.engine.SetIngressSession(r.ID, sk)
			e.engine.SetEgressSession(r.ID, sk)
			return nil
		}
	}
	return nil
}

// LoadStaticRoute registers one route from endpoint JSON bootstrap.
func (e *Endpoint) LoadStaticRoute(r rt.Route) error {
	return e.applyRoute(r, false)
}

// UpsertRoute updates/creates one route in runtime control-plane and immediately binds
// currently active owner session (if present) as ingress+egress session.
func (e *Endpoint) UpsertRoute(r rt.Route) error {
	return e.applyRoute(r, true)
}

// RemoveRoute deletes one route in runtime control-plane.
func (e *Endpoint) RemoveRoute(id rt.RouteID) {
	if id == 0 {
		e.controlErrors.Add(1)
		return
	}
	e.engine.RemoveRoute(id)
	e.controlRemoveOK.Add(1)
	e.sessMu.Lock()
	delete(e.routeOwners, id)
	e.sessMu.Unlock()
}
