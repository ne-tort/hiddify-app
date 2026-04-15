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
	if e.routeOwners == nil {
		e.routeOwners = make(map[rt.RouteID]string)
	}
	if e.ownerRoutes == nil {
		e.ownerRoutes = make(map[string]map[rt.RouteID]struct{})
	}
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
	if prevOwner != "" && prevOwner != r.Owner {
		if routeSet, ok := e.ownerRoutes[prevOwner]; ok {
			delete(routeSet, r.ID)
			if len(routeSet) == 0 {
				delete(e.ownerRoutes, prevOwner)
			}
		}
	}
	if _, ok := e.ownerRoutes[r.Owner]; !ok {
		e.ownerRoutes[r.Owner] = make(map[rt.RouteID]struct{})
	}
	e.ownerRoutes[r.Owner][r.ID] = struct{}{}
	e.sessMu.Unlock()
	e.refMu.Lock()
	defer e.refMu.Unlock()
	if e.activeOwnerSession == nil {
		e.activeOwnerSession = make(map[string]rt.SessionKey)
	}
	if sk, ok := e.activeOwnerSession[r.Owner]; ok {
		e.engine.SetIngressSession(r.ID, sk)
		e.engine.SetEgressSession(r.ID, sk)
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
	owner := e.routeOwners[id]
	delete(e.routeOwners, id)
	if owner != "" {
		if routeSet, ok := e.ownerRoutes[owner]; ok {
			delete(routeSet, id)
			if len(routeSet) == 0 {
				delete(e.ownerRoutes, owner)
			}
		}
	}
	e.sessMu.Unlock()
}
