# Development workflow

This project is in testing. Do not build release binaries, desktop applications,
installers or distribution archives unless the user explicitly asks for a build.
Source edits and relevant tests are allowed. Existing dist/ artifacts may therefore
be older than the source.

# Database and migrations
The normalized SQLite schema is baseline version 1. Never drop/reset an existing
agent database automatically, including during development. Initialize only an
empty, unversioned database; reject unsupported versions or fingerprints while
preserving their data. Future schema changes require explicit ordered migrations,
a schema-version increment in `PRAGMA user_version`, and preservation/rollback
tests. Application release versions are independent of the database version.

# Before pushing to GitHub
Remind the user to remove all temporary/generated builds before a GitHub push.
Keep builds available while they are testing. Verify that `dist/` stays ignored
and that no generated binaries, installers or archives are staged or tracked.
