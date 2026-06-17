# Group Management

A group is a named collection of users and service accounts that share
access to a set of permissions assigned by the group manager, an
administrative user. This keeps permission management consistent and
auditable; for a full description of how groups fit into the Workbench
access model, see the [Managing Users and Permissions](index.md) page.

## Creating a Group

You can create a group in the Workbench console or at the command line.

To create a group in the Administration page of the Workbench console,
select the `Settings` icon, choose the `Groups` tab, and then select
`+ Create Group`.

![Adding a group](../../images/create_group.png)

Provide the following details for the new group:

- Enter a group name in the Name field.
- Enter a description in the Description field.

Then, select `Create` to save the new group.

![Group List](../../images/group_list_console.png)

To add a member to the new group, use the down-arrow to the left of the
group name to expand the group description. Then, select the
`+ Add Member` icon at the far right of the entry:

![Adding a member to a group](../../images/select_add_member.png)

When the `Add member` popup opens, the Workbench prompts you for the new
member:

![Adding a member](../../images/add_member.png)

Complete the popup to add a user to the group:

- Select either the `User` or `Group` radio button to indicate whether
  the new group member is an individual member or a sub-group.
- Use the `Select User` drop-down to select the user account or group
  that the Workbench adds to the group.

Then, select `Add` to add the member and close the popup.

You can also create a group at the command line. In the following
example, the `-add-group` command creates a group named `dba-team`; the
`-group` flag supplies the group name:

```bash
./bin/ai-dba-server -add-group -group dba-team
```

The command confirms the new group and reports its assigned identifier:

```console
Group 'dba-team' created successfully (ID: 3)
```

You can add members at the command line with the `-add-member` command.
Use the `-username` flag to add a user or service account; use the
`-member-group` flag to add a nested group. You must specify only one of
these flags; the command rejects both flags if used together.

In the following example, the `-add-group` command creates the
`dba-team` group; the `-add-member` command then adds the user `alice`
to it:

```bash
./bin/ai-dba-server -add-group -group dba-team
./bin/ai-dba-server -add-member -group dba-team -username alice
```

The command confirms the new membership:

```console
User 'alice' added to group 'dba-team'
```

In the following example, the `-add-group` command creates the
`readonly` group; the `-add-member` command then nests it inside
`dba-team`:

```bash
./bin/ai-dba-server -add-group -group readonly
./bin/ai-dba-server -add-member -group dba-team -member-group readonly
```

The command confirms the nested membership:

```console
Group 'readonly' added to group 'dba-team'
```

## Listing Groups

You can review the configured groups in the Workbench console or at the
command line.

Groups appear on the `Groups` tab of the Administration console, where
each row shows the group name and its assigned privileges.

You can also list groups at the command line. In the following example,
the `-list-groups` command displays every group with its identifier,
name, creation time, and description:

```bash
./bin/ai-dba-server -list-groups
```

The command prints the groups in a table:

```console
Groups:
================================================================================
ID     Name                 Created              Description
--------------------------------------------------------------------------------
3      dba-team             2026-06-17 09:42     Database administrators
4      readonly             2026-06-17 10:05     Read-only analysts
================================================================================
```

## Removing Members

You remove a member from a group at the command line with the
`-remove-member` command. Use the `-username` flag to remove a user or
service account; use the `-member-group` flag to remove a nested group.
You must specify exactly one of these flags.

In the following example, the `-remove-member` command removes the user
`alice` from the `dba-team` group:

```bash
./bin/ai-dba-server -remove-member -group dba-team -username alice
```

The command confirms the change:

```console
User 'alice' removed from group 'dba-team'
```

In the following example, the `-remove-member` command removes the
nested `readonly` group from the `dba-team` group:

```bash
./bin/ai-dba-server -remove-member -group dba-team -member-group readonly
```

## Deleting a Group

You can delete a group in the Workbench console or at the command line.

To delete a group in the console, open the `Groups` tab, and then select
the delete icon for the group you wish to remove. Confirm the deletion
when the console prompts you.

You can also delete a group at the command line. In the following
example, the `-delete-group` command removes the `dba-team` group; the
`-group` flag names the group to delete:

```bash
./bin/ai-dba-server -delete-group -group dba-team
```

Deleting a group removes all of its memberships and privilege
assignments; the system cannot recover these once the group is gone. The
command confirms the deletion:

```console
Group 'dba-team' deleted successfully
```
