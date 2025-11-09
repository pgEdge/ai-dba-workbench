# Claude Standing Instructions

> This document provides standing instructions for Claude Code when working on 
> this project. It supplements the design in DESIGN.md.

## Project Structure

* The pgEdge AI Workbench data collector is implemented in the /collector 
    directory.

* The pgEdge AI Workbench web client application is implemented in the /client 
    directory.

* The pgEdge AI Workbench MCP server is implemented in the /server directory.

* All three sub projects should follow the following base structure.

    * Comprehensive documentation files should be created in markdown format 
        under the /docs directory, for each sub-project, e.g. /docs/collector, 
        /docs/cli, /docs/client, and /docs/server, with a top level index in
        /docs

    * Documentation files in the /docs directory should always use lower 
        case filename.

    * Unit and integration tests should be created under the /tests 
        subdirectory of each project, e.g. /client/tests, /collector/tests, 
        and /server/tests, except where the language convention is to include
        unit tests in the same directory as the code they are testing.

    * Source code should be created under the /src subdirectory of each 
        project, e.g. /client/src, /collector/src, and /server/src.

## Documentation

    * A README.md file in the sub-project top level directory should provide a 
        very high level overview of the sub-project, including basic getting 
        started information for developers and users.

    * The documentation for each sub-project should have an index.md file 
        acting as the entry point for the reader, and linked from the README.md
        file for the sub-project. The top-level README.md file should link to 
        the README.md file for each sub-project.

    * When editing markdown files, always leave a blank line before the first 
        item in any list or sub-list to ensure the lists render properly in 
        tools such as mkdocs.

    * Wrap all markdown files at 79 characters or less.

## Tests

    * Unit and integration tests should be provided for each sub-project.

    * Tests should all be executable using "go test" or "npm test", as 
        appropriate for the specific sub-project.

    * All code functions and features should have automated tests to the extent
        possible, using mocking where required.

    * All tests should be run following any changes being made, taking care not
        to miss any error messages or warnings due to output redirection or
        truncation.

    * Temporary files created during test execution must be cleaned up when 
        the test run completes, except where they contain useful debugging 
        information, for example log files.

    * Existing tests should never be modified unless the functionality they are
        exercising has been changed, or to fix bugs or refactor code.

    * Ensure linting tests are included, and run under the standard test suites
        utilising locally installable tools.

    * Ensure coverage can be checked, using the standard test suites utilising
        locally installable tools.

    * DO NOT skip DB tests when testing new changes.

    * ALWAYS test new changes with "make test-all" in the top level project 
        directory before completing a task.

## Security

    * Always ensure isolation is maintained between user sessions.

    * Always ensure that database connections are only accessible to the users
        or tokens that own them.

    * Protect against injection attacks of any kind, at both the client and 
        server, the only exception being in an MCP Tool allowing arbitrary
        SQL queries to be executed.

    * Always follow industry best practices for defensive secure coding.

    * Always review any changes for security implications and report any 
        potential issues found.

## Code Style

    * Always use four spaces for indentation.

    * Always ensure the code is written in a way that is readable, extensible,
        and is appropriately modularised.

    * Always ensure code duplication is minimised, refactoring where needed.

    * When creating database migrations, always use COMMENT ON to describe
        the objects created.

    * Include the following copyright notice at the top of every source
      file:

        > /*-----------------------------------------------------------
        >  *
        >  * pgEdge AI Workbench
        >  *
        >  * Copyright (c) 2025, pgEdge, Inc.
        >  * This software is released under The PostgreSQL License
        >  *
        >  *-----------------------------------------------------------
        >  */