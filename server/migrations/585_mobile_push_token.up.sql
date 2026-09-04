-- Mobile push (K64): the Expo push tokens of a user's devices. One row per
-- (user, token); a token belongs to a device, so it moves with the user
-- across workspaces. No foreign key.
CREATE TABLE mobile_push_token (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL,
    token      TEXT NOT NULL,
    platform   TEXT NOT NULL DEFAULT 'ios',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
