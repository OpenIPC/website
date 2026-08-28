-- The compose environment creates openipc_development and grants www on it.
-- The suite needs more than that: openipc_test, plus one database per Minitest
-- worker (openipc_test-0, openipc_test-1, ... -- the suite is parallelized
-- across cores), and those are created by Rails at run time under the www user.
--
-- The backslash escapes the underscore so it is matched literally rather than
-- as MySQL's single-character wildcard; without it the grant would also cover
-- databases like "openipcXfoo".
CREATE DATABASE IF NOT EXISTS openipc_test
  CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci;
GRANT ALL PRIVILEGES ON `openipc\_%`.* TO 'www'@'%';
FLUSH PRIVILEGES;
