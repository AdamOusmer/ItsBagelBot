-- Read-only verification for BagelBot service/schema isolation.
--
-- Expected result set:
--   users_svc          -> bagel_users only
--   commands_svc       -> bagel_commands only
--   modules_svc        -> bagel_modules only
--   transactions_svc   -> bagel_transactions only
-- and only SELECT, INSERT, UPDATE, DELETE privileges.

SELECT
  GRANTEE,
  TABLE_SCHEMA,
  GROUP_CONCAT(PRIVILEGE_TYPE ORDER BY PRIVILEGE_TYPE SEPARATOR ', ') AS privileges
FROM information_schema.SCHEMA_PRIVILEGES
WHERE GRANTEE IN (
  '''users_svc''@''%''',
  '''commands_svc''@''%''',
  '''modules_svc''@''%''',
  '''transactions_svc''@''%'''
)
GROUP BY GRANTEE, TABLE_SCHEMA
ORDER BY GRANTEE, TABLE_SCHEMA;

-- Any rows here are violations: runtime service users have DDL, grant, or
-- cross-schema access.
SELECT
  GRANTEE,
  TABLE_SCHEMA,
  PRIVILEGE_TYPE
FROM information_schema.SCHEMA_PRIVILEGES
WHERE GRANTEE IN (
  '''users_svc''@''%''',
  '''commands_svc''@''%''',
  '''modules_svc''@''%''',
  '''transactions_svc''@''%'''
)
AND (
  PRIVILEGE_TYPE NOT IN ('SELECT', 'INSERT', 'UPDATE', 'DELETE')
  OR (GRANTEE = '''users_svc''@''%''' AND TABLE_SCHEMA <> 'bagel_users')
  OR (GRANTEE = '''commands_svc''@''%''' AND TABLE_SCHEMA <> 'bagel_commands')
  OR (GRANTEE = '''modules_svc''@''%''' AND TABLE_SCHEMA <> 'bagel_modules')
  OR (GRANTEE = '''transactions_svc''@''%''' AND TABLE_SCHEMA <> 'bagel_transactions')
)
ORDER BY GRANTEE, TABLE_SCHEMA, PRIVILEGE_TYPE;
