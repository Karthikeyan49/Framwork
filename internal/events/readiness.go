package events

import (
	"context"
	"database/sql"
)

// Ready inspects contracts, not tenant event contents. Missing migrations,
// disabled envelope/immutability triggers, unsafe RLS or grants fail closed.
func Ready(ctx context.Context, db *sql.DB, role string) error {
	if db == nil {
		return ErrDelivery
	}
	var ok bool
	// PostgreSQL may inline/check SQL functions in unused CASE branches when a
	// prepared statement adopts a generic plan. Only a worker session may even
	// plan the worker-only authority query; do not grant these ports to the API.
	if role == "framework_worker" {
		if err := db.QueryRowContext(ctx, `SELECT control.worker_principal() AND control.worker_grants_safe()`).Scan(&ok); err != nil || !ok {
			return ErrDelivery
		}
	} else if role == "framework_app" || role == "framework_dispatcher" {
		if err := db.QueryRowContext(ctx, `SELECT session_user::text=$1::text`, role).Scan(&ok); err != nil || !ok {
			return ErrDelivery
		}
	} else {
		return ErrDelivery
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*)=1 AND coalesce(bool_and(p.proowner='framework_schema_owner'::regrole AND p.prokind='f' AND p.provolatile='s' AND p.prosecdef AND p.proconfig @> ARRAY['search_path=pg_catalog']),false) FROM pg_catalog.pg_proc p WHERE p.oid=to_regprocedure('control.event_delivery_policy()')`).Scan(&ok); err != nil || !ok {
		return ErrDelivery
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*)=1 AND coalesce(bool_and(version=$1 AND lease_ms=$2 AND receipt_ms=$3 AND max_attempts=$4 AND retry_base_ms=$5 AND retry_cap_ms=$6),false) FROM control.event_delivery_policy()`, DeliveryPolicyVersion, PublishLease.Milliseconds(), ReceiptWindow.Milliseconds(), MaxDeliveryAttempts, RetryBase.Milliseconds(), RetryCap.Milliseconds()).Scan(&ok); err != nil || !ok {
		return ErrDelivery
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*)=1 AND coalesce(bool_and(p.proowner='framework_schema_owner'::regrole AND p.prokind='f' AND p.provolatile='i' AND p.proisstrict AND NOT p.prosecdef
 AND p.prorettype='interval'::regtype AND p.proconfig @> ARRAY['search_path=pg_catalog']
 AND NOT has_function_privilege(p.oid,'EXECUTE')
 AND NOT has_function_privilege('framework_app',p.oid,'EXECUTE') AND NOT has_function_privilege('framework_worker',p.oid,'EXECUTE') AND NOT has_function_privilege('framework_dispatcher',p.oid,'EXECUTE')),false)
 FROM pg_catalog.pg_proc p WHERE p.oid=to_regprocedure('control.event_retry_delay(integer)')`).Scan(&ok); err != nil || !ok {
		return ErrDelivery
	}
	err := db.QueryRowContext(ctx, `WITH event_tables AS(
 SELECT c.oid,c.relrowsecurity,c.relforcerowsecurity,c.relowner,n.nspname||'.'||c.relname AS name FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname||'.'||c.relname IN('runtime.event_sequences','runtime.event_envelopes','runtime.event_inbox','control.event_deliveries','control.event_attempts','control.event_replays','control.event_quarantine','runtime.outbox_intents'))
 SELECT true
 AND (SELECT count(*)=8 AND bool_and(relrowsecurity AND relforcerowsecurity AND relowner='framework_schema_owner'::regrole)
 FROM event_tables)
 AND (SELECT count(*)=5 AND bool_and(tgenabled='O') FROM pg_catalog.pg_trigger JOIN event_tables t ON t.oid=tgrelid WHERE NOT tgisinternal AND
 (t.name='runtime.outbox_intents' AND tgname IN('freeze_event','immutable_intent') OR t.name='runtime.event_envelopes' AND tgname='immutable_envelope' OR t.name='runtime.event_inbox' AND tgname='immutable_inbox' OR t.name='control.event_replays' AND tgname='immutable_replay'))
 AND CASE WHEN $1='framework_app' THEN
 (SELECT count(*)=11 AND bool_and(has_column_privilege(attrelid,attname,'INSERT')) FROM pg_catalog.pg_attribute WHERE attrelid=(SELECT oid FROM event_tables WHERE name='runtime.outbox_intents') AND NOT attisdropped AND attname IN('tenant_id','id','app_id','metadata_revision','ir_hash','entity_id','record_id','record_version','event_type','request_id','traceparent'))
 AND NOT has_column_privilege((SELECT oid FROM event_tables WHERE name='runtime.outbox_intents'),'actor_id','INSERT')
 AND NOT has_column_privilege((SELECT oid FROM event_tables WHERE name='runtime.outbox_intents'),'occurred_at','INSERT') ELSE true END
 AND CASE $1 WHEN 'framework_worker' THEN has_function_privilege(to_regprocedure('control.resolve_event_v2(uuid,uuid,text,text,bigint,uuid)'),'EXECUTE') AND has_function_privilege(to_regprocedure('control.complete_event(uuid,uuid,text)'),'EXECUTE') AND has_function_privilege(to_regprocedure('control.quarantine_event(text,text)'),'EXECUTE') AND has_table_privilege((SELECT oid FROM event_tables WHERE name='runtime.event_envelopes'),'SELECT')
 AND has_table_privilege((SELECT oid FROM event_tables WHERE name='runtime.event_inbox'),'SELECT')
 AND (SELECT count(*)=4 AND bool_and(has_column_privilege(attrelid,attname,'INSERT')) FROM pg_catalog.pg_attribute WHERE attrelid=(SELECT oid FROM event_tables WHERE name='runtime.event_inbox') AND NOT attisdropped AND attname IN('tenant_id','consumer_id','event_id','body_hash'))
 AND NOT has_table_privilege((SELECT oid FROM event_tables WHERE name='runtime.event_inbox'),'UPDATE,DELETE,TRUNCATE') AND NOT has_any_column_privilege((SELECT oid FROM event_tables WHERE name='runtime.event_inbox'),'UPDATE')
 WHEN 'framework_dispatcher' THEN has_function_privilege(to_regprocedure('control.claim_event(uuid)'),'EXECUTE') AND has_function_privilege(to_regprocedure('control.finish_event(uuid,uuid,bigint,uuid,boolean)'),'EXECUTE') AND NOT has_any_column_privilege((SELECT oid FROM event_tables WHERE name='runtime.event_envelopes'),'SELECT')
 WHEN 'framework_app' THEN NOT has_any_column_privilege((SELECT oid FROM event_tables WHERE name='runtime.event_envelopes'),'SELECT') ELSE false END`, role).Scan(&ok)
	if err != nil || !ok {
		return ErrDelivery
	}
	return nil
}
