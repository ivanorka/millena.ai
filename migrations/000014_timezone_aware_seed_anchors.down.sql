-- This migration only corrects future execution instants. Reconstructing the
-- former process-time-dependent values would overwrite valid schedules, so the
-- safe rollback is intentionally a no-op.
SELECT 1;
