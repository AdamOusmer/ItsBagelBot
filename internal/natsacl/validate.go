// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package natsacl

import "fmt"

func validate(acl *ACL, keys *Keys) error {
	if err := validateAccountReferences(acl); err != nil {
		return err
	}
	if err := validateAccountKeys(acl, keys); err != nil {
		return err
	}
	if err := validateRoleKeys(acl, keys); err != nil {
		return err
	}
	return validateDuplicateRoles(acl, keys)
}

func validateAccountReferences(acl *ACL) error {
	if err := validateSystemAccountReference(acl); err != nil {
		return err
	}
	return validateImportReferences(acl)
}

func validateSystemAccountReference(acl *ACL) error {
	if acl.SystemAccount == "" {
		return nil
	}
	if _, ok := acl.Accounts[acl.SystemAccount]; !ok {
		return fmt.Errorf("%w: system_account %q", ErrUnknownAccount, acl.SystemAccount)
	}
	return nil
}

func validateImportReferences(acl *ACL) error {
	for name, spec := range acl.Accounts {
		for _, imp := range spec.Imports {
			if _, ok := acl.Accounts[imp.From]; !ok {
				return fmt.Errorf("%w: account %q imports from %q", ErrUnknownAccount, name, imp.From)
			}
		}
	}
	return nil
}

func validateAccountKeys(acl *ACL, keys *Keys) error {
	for name := range acl.Accounts {
		if _, ok := keys.Accounts[name]; !ok {
			return fmt.Errorf("%w: account %q", ErrMissingAccountKey, name)
		}
	}
	return nil
}

func validateRoleKeys(acl *ACL, keys *Keys) error {
	for account, spec := range acl.Accounts {
		roleKeys := keys.Roles[account]
		for role := range spec.Roles {
			if _, ok := roleKeys[role]; !ok {
				return fmt.Errorf("%w: account %q role %q", ErrMissingRoleKey, account, role)
			}
		}
	}
	return nil
}

func validateDuplicateRoles(acl *ACL, keys *Keys) error {
	for account, spec := range acl.Accounts {
		seen := make(map[string]string, len(spec.Roles))
		for role := range spec.Roles {
			key := keys.Roles[account][role]
			if other, ok := seen[key]; ok {
				return fmt.Errorf("%w: account %q roles %q and %q share a key", ErrDuplicateRole, account, other, role)
			}
			seen[key] = role
		}
	}
	return nil
}
