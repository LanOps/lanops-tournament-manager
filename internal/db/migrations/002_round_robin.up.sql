-- Allow 'round_robin' as a tournament format.
ALTER TABLE tournaments DROP CONSTRAINT tournaments_format_check;
ALTER TABLE tournaments ADD CONSTRAINT tournaments_format_check
    CHECK (format IN ('single_elimination', 'double_elimination', 'round_robin'));
