-- The leaderboard is opt-out: every member is on it unless they
-- untick "Show me on the leaderboard" on /club. Until now it was opt-in, and
-- nobody had a reason to tick it while nobody had stars, so the board stayed
-- empty. Existing members join it too; the box still takes them off.
ALTER TABLE club_members ALTER COLUMN listed SET DEFAULT true;
UPDATE club_members SET listed = true WHERE NOT listed;
