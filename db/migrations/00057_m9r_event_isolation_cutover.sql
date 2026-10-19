-- ADR063 cutover correction. Drain owned emitters/consumers/dispatchers first.
-- A live old reader/binder makes this fail immediately, without partial changes.
-- +goose Up
LOCK TABLE control.worker_context,runtime.event_envelopes,runtime.event_inbox IN ACCESS EXCLUSIVE MODE NOWAIT;

CREATE POLICY event_read_committed ON runtime.event_envelopes AS RESTRICTIVE TO framework_worker
 USING(current_setting('transaction_isolation')='read committed')
 WITH CHECK(current_setting('transaction_isolation')='read committed');
CREATE POLICY event_read_committed ON runtime.event_inbox AS RESTRICTIVE TO framework_worker
 USING(current_setting('transaction_isolation')='read committed')
 WITH CHECK(current_setting('transaction_isolation')='read committed');

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'forward-only event isolation cutover; drain owned processes and forward-fix'; END $$;
-- +goose StatementEnd
