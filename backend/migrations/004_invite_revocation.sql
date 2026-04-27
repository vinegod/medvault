-- 004_invite_revocation.sql
-- Add a revoked_at column so owners can kill an invite code without
-- waiting for it to expire (e.g., the code was shared with the wrong
-- person). The Join handler now treats revoked codes the same as
-- expired ones — uniform "invalid or expired" 400 response, no leak
-- of which case it is.

ALTER TABLE household_invites ADD COLUMN revoked_at DATETIME;
