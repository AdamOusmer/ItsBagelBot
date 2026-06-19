-- BagelBot MySQL/HeatWave hardening runbook.
--
-- Apply with an administrative database user during a maintenance window.
-- The application services must run with DB_AUTO_MIGRATE=false before the
-- REVOKE statements are applied. Keep DDL privileges on a separate migrator
-- user or run migrations through an operator-controlled job.
--
-- Security model:
--   * One schema per data-owning service.
--   * One runtime user per service, restricted to DML on that service's schema.
--   * One optional migrator user per schema, used only by controlled migration
--     jobs and never by application pods.
--   * No service user receives privileges on another service's schema.
--   * No service user receives global privileges.

-- 1. Enforce encrypted transport at the database endpoint.
-- Managed Oracle controls may expose this as a DB system parameter instead of
-- permitting SET PERSIST directly. If SET PERSIST is rejected, set
-- require_secure_transport=ON in the HeatWave/MySQL configuration.
SET PERSIST require_secure_transport = ON;

-- 2. Create schemas explicitly so ownership is deliberate.
CREATE DATABASE IF NOT EXISTS `bagel_users`
  CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;
CREATE DATABASE IF NOT EXISTS `bagel_commands`
  CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;
CREATE DATABASE IF NOT EXISTS `bagel_modules`
  CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;
CREATE DATABASE IF NOT EXISTS `bagel_transactions`
  CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

-- 3. Runtime service users should not own DDL in production.
-- They only need DML against their isolated schema once migrations are external.
--
-- If these users do not exist yet, create them with generated passwords and put
-- those secrets only in the matching Doppler project:
--   CREATE USER 'users_svc'@'%' IDENTIFIED BY '<generated>';
--   CREATE USER 'commands_svc'@'%' IDENTIFIED BY '<generated>';
--   CREATE USER 'modules_svc'@'%' IDENTIFIED BY '<generated>';
--   CREATE USER 'transactions_svc'@'%' IDENTIFIED BY '<generated>';
--
-- Revoke broad grants first. Some statements may be no-ops depending on current
-- grants; keep the final SHOW GRANTS verification as the source of truth.
REVOKE ALL PRIVILEGES, GRANT OPTION FROM `users_svc`@`%`;
REVOKE ALL PRIVILEGES, GRANT OPTION FROM `commands_svc`@`%`;
REVOKE ALL PRIVILEGES, GRANT OPTION FROM `modules_svc`@`%`;
REVOKE ALL PRIVILEGES, GRANT OPTION FROM `transactions_svc`@`%`;

GRANT SELECT, INSERT, UPDATE, DELETE ON `bagel_users`.* TO `users_svc`@`%`;
GRANT SELECT, INSERT, UPDATE, DELETE ON `bagel_commands`.* TO `commands_svc`@`%`;
GRANT SELECT, INSERT, UPDATE, DELETE ON `bagel_modules`.* TO `modules_svc`@`%`;
GRANT SELECT, INSERT, UPDATE, DELETE ON `bagel_transactions`.* TO `transactions_svc`@`%`;

-- 4. Optional migrator users. Keep these credentials out of service pods.
-- Use separate generated passwords and store them only where the migration job
-- can read them.
--   CREATE USER 'users_migrator'@'%' IDENTIFIED BY '<generated>';
--   CREATE USER 'commands_migrator'@'%' IDENTIFIED BY '<generated>';
--   CREATE USER 'modules_migrator'@'%' IDENTIFIED BY '<generated>';
--   CREATE USER 'transactions_migrator'@'%' IDENTIFIED BY '<generated>';
--
-- GRANT SELECT, INSERT, UPDATE, DELETE, CREATE, DROP, REFERENCES, INDEX, ALTER
--   ON `bagel_users`.* TO `users_migrator`@`%`;
-- GRANT SELECT, INSERT, UPDATE, DELETE, CREATE, DROP, REFERENCES, INDEX, ALTER
--   ON `bagel_commands`.* TO `commands_migrator`@`%`;
-- GRANT SELECT, INSERT, UPDATE, DELETE, CREATE, DROP, REFERENCES, INDEX, ALTER
--   ON `bagel_modules`.* TO `modules_migrator`@`%`;
-- GRANT SELECT, INSERT, UPDATE, DELETE, CREATE, DROP, REFERENCES, INDEX, ALTER
--   ON `bagel_transactions`.* TO `transactions_migrator`@`%`;

-- 5. Row invariants and index cleanup observed in production on 2026-06-19.
ALTER TABLE `bagel_users`.`users`
  MODIFY COLUMN `banned` tinyint(1) NOT NULL DEFAULT 0,
  DROP INDEX `user_id_is_active`;

ALTER TABLE `bagel_users`.`delegations`
  DROP INDEX `delegation_token`,
  ADD INDEX `delegation_owner_created_id` (`owner_id`, `created_at`, `id`),
  ADD INDEX `delegation_delegate_consumed_created_id` (`delegate_id`, `consumed_at`, `created_at`, `id`);

ALTER TABLE `bagel_users`.`admin_audits`
  DROP INDEX `adminaudit_created_at`,
  DROP INDEX `adminaudit_actor_id`,
  ADD INDEX `adminaudit_created_id` (`created_at`, `id`),
  ADD INDEX `adminaudit_actor_created_id` (`actor_id`, `created_at`, `id`);

ALTER TABLE `bagel_users`.`admin_users`
  ADD INDEX `adminuser_role_active` (`role`, `active`);

-- 6. Confirm least privilege after revocation. Expected runtime grants:
--     USAGE on *.*
--     SELECT, INSERT, UPDATE, DELETE on only the matching schema.
-- There must be no CREATE, DROP, ALTER, INDEX, REFERENCES and no grants on
-- another bagel_* schema.
SHOW GRANTS FOR `users_svc`@`%`;
SHOW GRANTS FOR `commands_svc`@`%`;
SHOW GRANTS FOR `modules_svc`@`%`;
SHOW GRANTS FOR `transactions_svc`@`%`;

SELECT GRANTEE, TABLE_SCHEMA, PRIVILEGE_TYPE
FROM information_schema.SCHEMA_PRIVILEGES
WHERE GRANTEE IN (
  '''users_svc''@''%''',
  '''commands_svc''@''%''',
  '''modules_svc''@''%''',
  '''transactions_svc''@''%'''
)
ORDER BY GRANTEE, TABLE_SCHEMA, PRIVILEGE_TYPE;
