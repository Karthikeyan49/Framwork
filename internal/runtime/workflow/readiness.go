package workflow

import (
	"context"
	"database/sql"
	"errors"
)

func Ready(ctx context.Context, db *sql.DB, expected string) error {
	if db == nil || !includes([]string{"framework_app", "framework_worker"}, expected) {
		return ErrInvalid
	}
	var role string
	var safe bool
	if e := db.QueryRowContext(ctx, `SELECT session_user,NOT rolsuper AND NOT rolbypassrls FROM pg_catalog.pg_roles WHERE rolname=session_user`).Scan(&role, &safe); e != nil || (expected == "framework_app" && role != expected) || !safe {
		return ErrDenied
	}
	var valid bool
	e := db.QueryRowContext(ctx, `SELECT
 (SELECT count(*)=6 AND bool_and(c.relrowsecurity AND c.relforcerowsecurity AND r.rolname='framework_schema_owner' AND NOT r.rolcanlogin AND NOT r.rolsuper AND NOT r.rolbypassrls) FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace JOIN pg_catalog.pg_roles r ON r.oid=c.relowner WHERE n.nspname='runtime' AND c.relname IN('workflow_bindings','workflow_instances','workflow_history','workflow_receipts','workflow_signals','workflow_outbox'))
 AND (SELECT count(*)=5 FROM pg_catalog.pg_trigger WHERE tgenabled='O' AND NOT tgisinternal AND tgfoid='control.immutable_artifact()'::regprocedure AND (tgrelid,tgname) IN(('runtime.workflow_bindings'::regclass,'immutable_workflow_binding'),('runtime.workflow_history'::regclass,'immutable_workflow_history'),('runtime.workflow_receipts'::regclass,'immutable_workflow_receipt'),('runtime.workflow_signals'::regclass,'immutable_workflow_signal'),('runtime.workflow_outbox'::regclass,'immutable_workflow_outbox')))
 AND EXISTS(SELECT 1 FROM pg_catalog.pg_trigger WHERE tgrelid='runtime.workflow_bindings'::regclass AND tgname='route_workflow' AND tgenabled='O' AND tgfoid='runtime.route_workflow()'::regprocedure)
 AND EXISTS(SELECT 1 FROM pg_catalog.pg_trigger WHERE tgrelid='runtime.workflow_bindings'::regclass AND tgname='workflow_data_pin' AND tgenabled='O' AND tgfoid='runtime.check_workflow_data_pin()'::regprocedure)
 AND EXISTS(SELECT 1 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_roles r ON r.oid=c.relowner WHERE c.oid='control.workflow_jobs'::regclass AND c.relrowsecurity AND c.relforcerowsecurity AND r.rolname='framework_schema_owner' AND NOT r.rolcanlogin AND NOT r.rolsuper AND NOT r.rolbypassrls)
 AND EXISTS(SELECT 1 FROM pg_catalog.pg_class c WHERE c.oid='control.workflow_attempts'::regclass AND c.relrowsecurity AND c.relforcerowsecurity AND c.relowner='framework_schema_owner'::regrole)
 AND EXISTS(SELECT 1 FROM pg_catalog.pg_trigger WHERE tgrelid='control.workflow_attempts'::regclass AND tgname='immutable_workflow_attempt' AND tgenabled='O' AND tgfoid='control.immutable_artifact()'::regprocedure)
 AND EXISTS(SELECT 1 FROM pg_catalog.pg_trigger WHERE tgrelid='control.workflow_jobs'::regclass AND tgname='workflow_attempt_finished' AND tgenabled='O' AND tgfoid='control.workflow_attempt_finished()'::regprocedure)
 AND has_function_privilege('control.schedule_workflow(uuid,uuid)','EXECUTE')
 AND has_function_privilege('control.workflow_route_state(uuid,uuid)','EXECUTE')
 AND has_table_privilege('runtime.workflow_bindings','SELECT')
 AND has_column_privilege('runtime.workflow_instances','status','UPDATE')
 AND has_column_privilege('runtime.workflow_history','event','INSERT')
 AND has_column_privilege('runtime.workflow_receipts','output','INSERT')
 AND has_column_privilege('runtime.workflow_outbox','body','INSERT')
 AND NOT has_table_privilege('runtime.workflow_bindings','UPDATE')
 AND NOT has_table_privilege('runtime.workflow_history','DELETE')
 AND NOT has_table_privilege('control.workflow_jobs','SELECT')`).Scan(&valid)
	if e != nil || !valid {
		return errors.New("workflow storage contract unavailable")
	}
	if expected == "framework_worker" {
		if e = db.QueryRowContext(ctx, `SELECT control.worker_principal() AND NOT has_function_privilege('control.claim_workflow(uuid)','EXECUTE') AND has_function_privilege('control.assigned_workflow()','EXECUTE') AND has_function_privilege('control.bind_worker_job(uuid,uuid,text,bigint,uuid,text)','EXECUTE') AND has_function_privilege('runtime.worker_source_record()','EXECUTE') AND NOT has_table_privilege('runtime.records','SELECT,INSERT,UPDATE,DELETE') AND NOT has_any_column_privilege('runtime.records','SELECT,INSERT,UPDATE') AND NOT has_table_privilege('control.worker_context','SELECT,INSERT,UPDATE,DELETE') AND has_function_privilege('control.check_workflow_lease(uuid,uuid,text,bigint,uuid)','EXECUTE')`).Scan(&valid); e != nil || !valid {
			return ErrDenied
		}
	}
	// Every required column, not a representative grant. Table grants are also
	// considered by PostgreSQL's has_column_privilege implementation.
	grants := []struct {
		table, privilege string
		columns          []string
	}{
		{"runtime.workflow_instances", "UPDATE", []string{"state_id", "status", "version", "attempt", "entered_at", "due_at", "termination", "failure"}},
		{"runtime.workflow_history", "INSERT", []string{"tenant_id", "instance_id", "sequence", "state_id", "status", "event", "step_key", "result_hash"}},
		{"runtime.workflow_receipts", "INSERT", []string{"tenant_id", "instance_id", "step_key", "activity_ref", "request_hash", "output", "output_hash", "sequence", "compensation_ref"}},
		{"runtime.workflow_outbox", "INSERT", []string{"tenant_id", "instance_id", "sequence", "body", "body_hash"}},
	}
	if expected == "framework_app" {
		grants = append(grants,
			struct {
				table, privilege string
				columns          []string
			}{"runtime.workflow_bindings", "INSERT", []string{"tenant_id", "id", "app_id", "metadata_revision", "ir_hash", "ir_body", "workflow_id", "workflow_version", "plan_hash", "plan_body", "record_id", "record_version", "inputs", "input_hash", "start_key", "request_hash", "request_id", "traceparent"}},
			struct {
				table, privilege string
				columns          []string
			}{"runtime.workflow_instances", "INSERT", []string{"tenant_id", "id", "state_id", "status", "version", "deadline"}},
			struct {
				table, privilege string
				columns          []string
			}{"runtime.workflow_signals", "INSERT", []string{"tenant_id", "instance_id", "idempotency_key", "request_hash", "sequence"}},
		)
	}
	for _, g := range grants {
		for _, column := range g.columns {
			if e = db.QueryRowContext(ctx, `SELECT has_column_privilege($1,$2,$3)`, g.table, column, g.privilege).Scan(&valid); e != nil || !valid {
				return ErrDenied
			}
		}
	}
	for _, table := range []string{"runtime.workflow_bindings", "runtime.workflow_instances", "runtime.workflow_history", "runtime.workflow_receipts", "runtime.workflow_signals", "runtime.workflow_outbox"} {
		if e = db.QueryRowContext(ctx, `SELECT has_table_privilege($1,'SELECT') AND NOT has_table_privilege($1,'DELETE')`, table).Scan(&valid); e != nil || !valid {
			return ErrDenied
		}
	}
	for _, table := range []string{"runtime.workflow_bindings", "runtime.workflow_history", "runtime.workflow_receipts", "runtime.workflow_signals", "runtime.workflow_outbox"} {
		if e = db.QueryRowContext(ctx, `SELECT NOT EXISTS(SELECT 1 FROM pg_catalog.pg_attribute WHERE attrelid=$1::regclass AND attnum>0 AND NOT attisdropped AND has_column_privilege(attrelid,attname,'UPDATE'))`, table).Scan(&valid); e != nil || !valid {
			return ErrDenied
		}
	}
	if e = db.QueryRowContext(ctx, `SELECT NOT has_column_privilege('runtime.workflow_bindings','actor_id','INSERT') AND NOT has_column_privilege('runtime.workflow_history','actor_id','INSERT') AND NOT has_column_privilege('runtime.workflow_signals','actor_id','INSERT') AND NOT has_column_privilege('runtime.workflow_instances','deadline','UPDATE') AND NOT has_table_privilege('control.workflow_attempts','SELECT')`).Scan(&valid); e != nil || !valid {
		return ErrDenied
	}
	return nil
}
