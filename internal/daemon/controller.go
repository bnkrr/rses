package daemon

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bnkrr/rses/internal/session"
)

type operationSummary struct {
	Results  map[string]string
	Statuses []session.Status
}

type operationTarget struct {
	ID   operationID
	Name string
	Deps []operationID
	Run  func(context.Context, eventEmitter) operationResult
}

type operationResult struct {
	ID      operationID
	Name    string
	Result  string
	Status  session.Status
	Failed  bool
	Skipped bool
	Error   string
}

func (s *Server) start(ctx context.Context, req Request, emit eventEmitter) operationSummary {
	targets := make([]operationTarget, 0, len(req.Specs))
	ids := specIDs(req.ProjectDir, req.Specs)
	for _, spec := range req.Specs {
		spec := spec
		id := operationIDForSpec(req.ProjectDir, spec)
		deps := dependencyIDs(spec.DependsOn, ids)
		targets = append(targets, operationTarget{
			ID:   id,
			Name: spec.Name,
			Deps: deps,
			Run: func(ctx context.Context, emit eventEmitter) operationResult {
				return s.startOne(ctx, id, req.ProjectDir, spec, req.Runtime, emit)
			},
		})
	}
	return s.runDAG(ctx, req.Action, req.Parallel, targets, true, emit)
}

func (s *Server) stop(ctx context.Context, req Request, emit eventEmitter) operationSummary {
	targets, err := s.stopTargets(req)
	if err != nil {
		_ = emit(OperationEvent{Type: "failed", Action: req.Action, Error: err.Error()})
		return operationSummary{}
	}
	return s.runDAG(ctx, req.Action, req.Parallel, targets, false, emit)
}

func (s *Server) status(ctx context.Context, req Request, emit eventEmitter) operationSummary {
	targets, err := s.statusTargets(req)
	if err != nil {
		_ = emit(OperationEvent{Type: "failed", Action: req.Action, Error: err.Error()})
		return operationSummary{}
	}
	return s.runDAG(ctx, req.Action, req.Parallel, targets, false, emit)
}

func (s *Server) logs(req Request) Response {
	ctx := context.Background()
	if len(req.Specs) == 1 {
		spec := req.Specs[0]
		plan, err := tmuxPlan(req.ProjectDir, spec, req.Lines)
		if err != nil {
			return Response{OK: false, Error: err.Error()}
		}
		log, err := runQueryPlan(ctx, spec, plan.LogScript, req.Runtime)
		if err != nil {
			return Response{OK: false, Error: trimError(log, err)}
		}
		return Response{OK: true, Log: log}
	}
	records, err := matchingRecords(req)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	if len(records) != 1 {
		return Response{OK: false, Error: "logs require exactly one configured or recorded session"}
	}
	record := records[0]
	spec := specFromRecord(record)
	plan, err := tmuxPlan(record.ProjectDir, spec, req.Lines)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	log, err := runQueryPlan(ctx, spec, plan.LogScript, req.Runtime)
	if err != nil {
		return Response{OK: false, Error: trimError(log, err)}
	}
	return Response{OK: true, Log: log}
}

func (s *Server) startOne(ctx context.Context, id operationID, projectDir string, spec session.Spec, runtime session.RuntimeOptions, emit eventEmitter) operationResult {
	unlock := s.lockSession(id)
	defer unlock()
	_ = emit(OperationEvent{Type: "progress", Name: spec.Name, Message: "starting " + spec.Name + "..."})
	st := tmuxStatus(ctx, projectDir, spec, runtime)
	if st.State == session.StateRunning {
		return successResult(id, "start", spec.Name, "already running", st, emit)
	}
	if st.State == session.StateUnknown && st.LastError != "" {
		return failedResult(id, "start", spec.Name, "failed: "+st.LastError, st, st.LastError, emit)
	}
	plan, err := tmuxPlan(projectDir, spec, 200)
	if err != nil {
		st.State = session.StateFailed
		st.LastError = err.Error()
		return failedResult(id, "start", spec.Name, "failed: "+err.Error(), st, err.Error(), emit)
	}
	out, err := runOperationPlan(ctx, spec, plan.StartScript, runtime)
	if err != nil {
		msg := strings.TrimSpace(out)
		if msg == "" {
			msg = err.Error()
		}
		st.State = session.StateFailed
		st.LastError = msg
		return failedResult(id, "start", spec.Name, "failed: "+msg, st, msg, emit)
	}
	if !spec.IsRemote() {
		_ = writeLocalRecord(plan, time.Now())
	}
	st = tmuxStatus(ctx, projectDir, spec, runtime)
	return successResult(id, "start", spec.Name, "started", st, emit)
}

func (s *Server) stopOne(ctx context.Context, id operationID, projectDir string, spec session.Spec, runtime session.RuntimeOptions, emit eventEmitter) operationResult {
	unlock := s.lockSession(id)
	defer unlock()
	_ = emit(OperationEvent{Type: "progress", Name: spec.Name, Message: "stopping " + spec.Name + "..."})
	result, st := stopSpec(ctx, projectDir, spec, runtime)
	if strings.HasPrefix(result, "failed:") {
		return failedResult(id, "stop", spec.Name, result, st, strings.TrimPrefix(result, "failed: "), emit)
	}
	return successResult(id, "stop", spec.Name, result, st, emit)
}

func (s *Server) statusOne(ctx context.Context, id operationID, projectDir string, spec session.Spec, runtime session.RuntimeOptions, emit eventEmitter) operationResult {
	unlock := s.lockSession(id)
	defer unlock()
	st := tmuxStatus(ctx, projectDir, spec, runtime)
	_ = emit(OperationEvent{Type: "status", Name: spec.Name, State: st.State, Status: &st})
	return operationResult{ID: id, Name: spec.Name, Status: st}
}

func successResult(id operationID, action, name, result string, st session.Status, emit eventEmitter) operationResult {
	_ = emit(OperationEvent{Type: "success", Action: action, Name: name, State: st.State, Status: &st, Message: result})
	return operationResult{ID: id, Name: name, Result: result, Status: st}
}

func failedResult(id operationID, action, name, result string, st session.Status, message string, emit eventEmitter) operationResult {
	_ = emit(OperationEvent{Type: "failed", Action: action, Name: name, State: st.State, Status: &st, Message: result, Error: message})
	return operationResult{ID: id, Name: name, Result: result, Status: st, Failed: true, Error: message}
}

func skippedResult(id operationID, action, name, reason string, emit eventEmitter) operationResult {
	result := "skipped: " + reason
	_ = emit(OperationEvent{Type: "skipped", Action: action, Name: name, Message: result, Error: reason})
	return operationResult{ID: id, Name: name, Result: result, Skipped: true, Error: reason}
}

func (s *Server) statusTargets(req Request) ([]operationTarget, error) {
	targets := []operationTarget{}
	seen := map[operationID]bool{}
	if req.IncludeConfigured {
		for _, spec := range req.Specs {
			if !spec.MatchesHost(req.Host) {
				continue
			}
			spec := spec
			id := operationIDForSpec(req.ProjectDir, spec)
			targets = append(targets, operationTarget{
				ID:   id,
				Name: spec.Name,
				Run: func(ctx context.Context, emit eventEmitter) operationResult {
					return s.statusOne(ctx, id, req.ProjectDir, spec, req.Runtime, emit)
				},
			})
			seen[id] = true
		}
	}
	records, err := matchingRecords(req)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		record := record
		id := operationIDForRecord(record)
		if seen[id] {
			continue
		}
		spec := specFromRecord(record)
		targets = append(targets, operationTarget{
			ID:   id,
			Name: record.Name,
			Run: func(ctx context.Context, emit eventEmitter) operationResult {
				return s.statusOne(ctx, id, record.ProjectDir, spec, req.Runtime, emit)
			},
		})
	}
	return targets, nil
}

func (s *Server) stopTargets(req Request) ([]operationTarget, error) {
	targets := []operationTarget{}
	seen := map[operationID]bool{}
	ids := specIDs(req.ProjectDir, req.Specs)
	waitFor := map[operationID][]operationID{}
	for _, spec := range req.Specs {
		id := operationIDForSpec(req.ProjectDir, spec)
		for _, dep := range spec.StopDependencies {
			if depID, ok := ids[dep]; ok {
				waitFor[depID] = append(waitFor[depID], id)
			}
		}
	}
	if req.IncludeConfigured {
		for _, spec := range req.Specs {
			if !spec.MatchesHost(req.Host) {
				continue
			}
			spec := spec
			id := operationIDForSpec(req.ProjectDir, spec)
			targets = append(targets, operationTarget{
				ID:   id,
				Name: spec.Name,
				Deps: waitFor[id],
				Run: func(ctx context.Context, emit eventEmitter) operationResult {
					return s.stopOne(ctx, id, req.ProjectDir, spec, req.Runtime, emit)
				},
			})
			seen[id] = true
		}
	}
	records, err := matchingRecords(req)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		record := record
		id := operationIDForRecord(record)
		if seen[id] {
			continue
		}
		spec := specFromRecord(record)
		targets = append(targets, operationTarget{
			ID:   id,
			Name: record.Name,
			Run: func(ctx context.Context, emit eventEmitter) operationResult {
				return s.stopOne(ctx, id, record.ProjectDir, spec, req.Runtime, emit)
			},
		})
	}
	return targets, nil
}

func dependencyIDs(names []string, ids map[string]operationID) []operationID {
	out := []operationID{}
	for _, name := range names {
		if id, ok := ids[name]; ok {
			out = append(out, id)
		}
	}
	return out
}

func specIDs(projectDir string, specs []session.Spec) map[string]operationID {
	out := make(map[string]operationID, len(specs))
	for _, spec := range specs {
		out[spec.Name] = operationIDForSpec(projectDir, spec)
	}
	return out
}

func (s *Server) runDAG(ctx context.Context, action string, parallel int, targets []operationTarget, failBlocks bool, emit eventEmitter) operationSummary {
	parallel = normalizeParallel(parallel)
	_ = emit(OperationEvent{Type: "begin", Action: action, Message: fmt.Sprintf("%s %d session(s)", action, len(targets))})
	results := map[string]string{}
	statuses := []session.Status{}
	if len(targets) == 0 {
		return operationSummary{Results: results, Statuses: statuses}
	}

	ops := map[operationID]operationTarget{}
	order := make([]operationID, 0, len(targets))
	for _, target := range targets {
		if previous, ok := ops[target.ID]; ok {
			_ = emit(OperationEvent{
				Type:  "failed",
				Error: fmt.Sprintf("sessions %q and %q both resolve to operation target %q", previous.Name, target.Name, target.ID.String()),
			})
			return operationSummary{Results: results, Statuses: statuses}
		}
		ops[target.ID] = target
		order = append(order, target.ID)
	}
	remaining := map[operationID]int{}
	dependents := map[operationID][]operationID{}
	blocked := map[operationID]string{}
	for _, target := range targets {
		for _, dep := range target.Deps {
			if _, ok := ops[dep]; !ok {
				continue
			}
			remaining[target.ID]++
			dependents[dep] = append(dependents[dep], target.ID)
		}
	}
	ready := []operationID{}
	for _, id := range order {
		if remaining[id] == 0 {
			ready = append(ready, id)
		}
	}

	type runResult struct {
		result operationResult
	}
	resultCh := make(chan runResult, len(targets))
	completed := map[operationID]bool{}
	running := 0
	completedCount := 0

	var complete func(operationResult)
	complete = func(res operationResult) {
		completed[res.ID] = true
		completedCount++
		if res.Result != "" {
			key := resultKey(results, res)
			results[key] = res.Result
		}
		if res.Status.Name != "" {
			statuses = append(statuses, res.Status)
		}
		for _, child := range dependents[res.ID] {
			if completed[child] {
				continue
			}
			if failBlocks && (res.Failed || res.Skipped) && blocked[child] == "" {
				blocked[child] = fmt.Sprintf("dependency %s failed", res.Name)
			}
			remaining[child]--
			if remaining[child] == 0 {
				if reason := blocked[child]; reason != "" {
					complete(skippedResult(child, action, ops[child].Name, reason, emit))
					continue
				}
				ready = append(ready, child)
			}
		}
	}

	for completedCount < len(targets) {
		for running < parallel && len(ready) > 0 && ctx.Err() == nil {
			id := ready[0]
			ready = ready[1:]
			target := ops[id]
			running++
			go func() {
				res := target.Run(ctx, emit)
				if res.ID == (operationID{}) {
					res.ID = target.ID
				}
				if res.Name == "" {
					res.Name = target.Name
				}
				resultCh <- runResult{result: res}
			}()
		}
		if running == 0 {
			for _, id := range order {
				if !completed[id] {
					complete(skippedResult(id, action, ops[id].Name, "dependency cycle or blocked dependency", emit))
				}
			}
			break
		}
		select {
		case res := <-resultCh:
			running--
			complete(res.result)
		case <-ctx.Done():
			for _, id := range order {
				if !completed[id] {
					complete(skippedResult(id, action, ops[id].Name, ctx.Err().Error(), emit))
				}
			}
		}
	}
	sortStatuses(statuses)
	return operationSummary{Results: results, Statuses: statuses}
}

func resultKey(results map[string]string, res operationResult) string {
	if _, ok := results[res.Name]; !ok {
		return res.Name
	}
	return res.ID.String()
}

func normalizeParallel(value int) int {
	if value <= 0 {
		return 4
	}
	if value > 32 {
		return 32
	}
	return value
}
