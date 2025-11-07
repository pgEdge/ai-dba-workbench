# pgEdge AI Workbench Design

> This document provides architectural context for Claude Code when working on
> this project. It supplements the instructions in CLAUDE.md.

pgEdge AI Server is a monitoring and management solution for pgEdge Enterprise
Postgres, running as a single node, single node with one or more read replica
servers (using PostgreSQL's binary replication), or multi-master distributed
clusters using the Spock replication engine, optionally with one or more read
replicas on the Spock nodes.

The functionality we can provide without integrated monitoring is severely 
limited in nature; it can only operate on data obtained in realtime from the 
managed systems, which prevents us from understanding usage and load patterns
which are often critical in diagnosing imminent problems, or diagnosing 
problems that occurred in the past, for example overnight.

It is therefore critical that any system we build should have the ability to
collect, store, retrieve, and age out metrics from the servers we are managing.

## Technology

The pgEdge AI server will be written in GoLang. This allows us to build the 
core of the server into a single binary that can easily be packaged and 
distributed in a variety of ways, without having any need to deal with runtime
dependencies. It also offers us the well known benefits of using GoLang, such
as memory management and a language design that lends itself to secure and 
less bug prone code, than for example, C or C++.

The pgEdge AI server will consist of two components; the server and collector
which together will be responsible for metric collection and storage into a
PostgreSQL database (the collector), and will function as an MCP server using
Server-Sent Events (SSE) over HTTP (for testing/non-production) and HTTPS
(the server). The server will not implement any AI related features itself;
along with storing monitoring data, it will implement tools and resources
that will enable the client to undertake operations such as updating the
server configuration or retrieving log files, and returning data sets, such
as history data from the metric datastore, or realtime data from the
monitored servers.

A simple CLI will be created, primarily intended for testing of the MCP 
server, with the ability to list tools, resources, and prompts, and to 
exercise them and display raw output from the MCP server.

Initially, we will build a simple frontend client application. This will be 
written in React, running under NodeJS, and use the MUI library for ease of 
development of a professional, and clean design. It will connect to the AI
server, and to either Anthropic's Claude or Ollama API's, utilising one of 
those to transform textual user requests into responses, based on information 
retrieved from the AI server.

The remainder of this document will focus on the pgEdge AI Server.

## Key Architecture

The following sections describe key architectural points of the design.

### Configuration

A command line option will be provided in the binaries to provide the path to 
the main configuration file. If this option is not provided by the user, the
configuration file will be read from the directory in which the binary is
stored.

Command line options will be provided for the following configuration 
parameters. Corresponding options may also be set in the configuration file. If
any options are provided in both locations, the command line will take 
priority, followed by the configuration file, and lastly, hard-coded default
values.

The following options will be provided.

MCP Server options (in the server binary only):

* tls - Enable HTTPS mode.
* tls_cert - The path to the certificate to use, when TLS is enabled.
* tls_key - The path to the key to use, when TLS is enabled.
* tls_chain - The path to the certificate chain to use, when TLS is enabled.

Datastore options (in both binaries):

* pg_host - The hostname or IP address of the PostgreSQL server.
* pg_hostaddr - The IP address of the PostgreSQL server (to avoid DNS lookups).
* pg_database - The name of the database to use.
* pg_username - The username with which to connect to PostgreSQL.
* pg_password_file - The path to a file containing the password (if required).
* pg_port - The port on which the PostgreSQL server is listening.
* pg_sslmode - The SSL mode to enable for the connection.
* pg_sslcert - The path to the client SSL certificate.
* pg_sslkey - The path to the client SSL key.
* pg_sslrootcert - The path to the client root SSL certificate.

Other options:

* server_secret - A per-installation string to use in encryption keys etc.
    (configuration file only)

All other configuration options that may be provided will be stored in the 
PostgreSQL datastore, in a suitable table.

### Authentication

Two types of authentication will be provided, both ultimately using bearer 
tokens to authenticate individual API calls.

User accounts will be stored in the PostgreSQL datastore, including the 
following attributes:

* Username
* Email address
* Full name
* Hashed (SHA256) password
* Password expiry timestamp
* Superuser status

A command line option will be provided in the server to allow the user to
create a initial accounts with the given username, with prompts to enter the 
additional required information.

An API will be provided as an MCP tool to allow a user to login. This will be
called by the client application, and if the username and password can be 
properly verified, and the password has not expired, a token will be issued to
the client for use in all future API calls. Each user may have multiple tokens
issued, which will be deleted upon logout (for which another API will be
provided), or after 24 hours. An API will be provided to allow the client to
request a new token at any time.

The system will also support service tokens. These will not be associated with
any user account, and include only a name, the expiry timestamp and superuser 
status, as well as a note in which the user can record the purpose of the 
token. A similar command line option will be provided to allow service 
tokens to be created.

Tool APIs will be provided to allow those with superuser permissions to add,
edit, update, and delete both user and service tokens.

### Monitored Server Connections

Tool APIs will be provided to create connections to monitored servers. These
may be "shared" connections, or private to either the user or service token
that creates them. Only users or tokens with superuser status can create
shared connections.

Each connection will take the the same options as the datastore database
connection, and will be stored in a table in the datastore. The one exception
will be the pg_password_file option that will be replaced with a pg_password
option. Any certificates used must to accessible on the AI Server's filesystem.
Passwords (if used) will be encrypted using a combination of the server_secret
and username or token name (for service tokens).

Note that the database name provided is used as a "maintenance database", 
similar to pgAdmin. It is used for the initial connection to the server, from
which additional accessible databases may be discovered and accessed using the
other connection parameters.

Each connection will have a unique numeric identifier (used as the primary 
key in the datastore), and a flag that indicates whether the server is to be
actively monitored or not.

### Monitoring

Monitoring will be performed using threads within the collector to avoid 
slowing the main program loop. A "probe" will consist of a SQL query that 
returns a set of metrics to log, along with an identifier for the PostgreSQL 
server, the database name, and any other required identifiers such as a table 
name.

The user will be able to configure (using an API tool), the collection interval
and lifetime of collected metrics for each probe. The data will be stored in a 
per-probe table, which is partitioned by week. A garbage collector thread will 
run daily and drop any partitions in which all the data is past the retention
time for that probe.

### MCP Resources

Resources will be provided to return data from all of the probes, for a given
time period with the given key information (server ID, database name etc).

Additional resources will be provided to return realtime snapshots of the same
data from any of the monitored servers.

### MCP Tools

In addition to the Tools already noted, additional tools will be provided to 
allow:

- The user to change their password.
- Reading of database server logs.
- Reading of postgresql.conf, pg_hba.conf, and pg_ident.conf.
- Execution of an arbitrary SQL query.
- ALTER SYSTEM SET...
- Stats on table and index bloat.

### Additional Functionality

If available, the system_stats and pg_stat_statements extensions will be used 
as an additional data source for probes and realtime data.

If Spock is installed, it's status tables will be used and as an additional
data source for probes and realtime data.

### Testing

Each sub-project will contain a test suite in the <project>/tests/ directory 
that will contain both unit and regression tests. Unit tests will exercise
individual code functions, and regression tests will test the functionality of
the built code. Tests will be written such that they run under "go test" or
"npm test" as appropriate. 100% coverage will be targetted.

A /tests directory in the top level of the project will provide end to end 
integration testing. The collector and server will be started in a temporary
test database, and the system exercised using both the CLI and web client.

### Future Enhancements

Role Based Access Controls may be added to limit access to specific Tools and
Resources to members of a hierarchical set of roles, in which a user or service
token can only access Tools and Resources to which a role of which they are a
member of has access. Roles may be members of other roles, and thus inherit 
their access.

Support for running multiple collectors may be added to provide high 
availability and/or load balancing.