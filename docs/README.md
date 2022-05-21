# What is it

This utility syncs duty users from specific `Opsgenie` schedules with user groups in `Slack`.

In addition to the on-call from `Opsgenie`, you can specify additional ones that will also be added to the user group.

## What is required for work

- `Opsgenie` api key with access:
  - `configuration access`
  - `read`
- `Slack` app OAuth token with scopes:
  - `usergroups:read`
  - `usergroups:write`
  - `users:read`
  - `users:read.email`
- Docker / golang

## Configuration example

```yaml
slack_user_group_name1:
  - opsgenie schedule name 1

slack_user_group_name2:
  - opsgenie schedule name 2
  - additional.user@num1
  - additional.user@num2
```

## Usage with docker

- Create a `config.yaml` in e.g. `/opt/opsgin` with the following content:

```yaml
slack_user_group_name1:
  - opsgenie schedule name 1
```

- Start the container by adding a directory with a configuration file:

```shell
docker run \
    -v /opt/opsgin:/opt/opsgin \
    -e OPSGIN_API_KEY=*** \
    -e OPSGIN_SLACK_API_KEY=*** \
    opsgin/opsgin:0.1-e6f2c10 sync
```

## Build from source code

```shell
go install -v github.com/opsgin/opsgin@0.1
opsgin
Synchronization of the on-duty Opsgenie with Slack user groups

Usage:
  opsgin [command]

Available Commands:
  help        Help about any command
  sync        Sync users

Flags:
      --config-file string   Set the configuration file name (default "config.yaml")
      --config-path string   Set the configuration file path (default "/etc/opsgin")
  -h, --help                 help for opsgin
      --log-format string    Set the log format: text, json (default "text")
      --log-level string     Set the log level: debug, info, warn, error, fatal (default "info")

Use "opsgin [command] --help" for more information about a command.
```
