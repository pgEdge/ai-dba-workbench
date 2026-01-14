# Cron Expression Format

The alerter uses standard 5-field cron expressions for scheduling blackout
periods. This document describes the supported syntax and provides examples
for common scheduling scenarios.

## Expression Format

A cron expression consists of five fields separated by spaces:

```
minute hour day-of-month month day-of-week
```

Each field specifies when the schedule should trigger:

| Field | Range | Description |
|-------|-------|-------------|
| minute | 0-59 | Minute of the hour |
| hour | 0-23 | Hour of the day (24-hour format) |
| day-of-month | 1-31 | Day of the month |
| month | 1-12 | Month of the year |
| day-of-week | 0-6 | Day of the week (0 = Sunday) |

## Supported Syntax

The cron parser supports the following syntax elements:

### Wildcards

The asterisk (`*`) matches any value in the field. In the following example,
the expression triggers every minute:

```
* * * * *
```

### Specific Values

A single number specifies an exact value. In the following example, the
expression triggers at 3:00 AM on the first day of each month:

```
0 3 1 * *
```

### Lists

Comma-separated values specify multiple options. In the following example,
the expression triggers at midnight on the 1st and 15th of each month:

```
0 0 1,15 * *
```

### Ranges

A hyphen specifies a range of values. In the following example, the expression
triggers every hour from 9 AM to 5 PM:

```
0 9-17 * * *
```

### Steps

A slash specifies step values. In the following example, the expression
triggers every 15 minutes:

```
*/15 * * * *
```

### Combined Syntax

You can combine ranges with steps. In the following example, the expression
triggers every other hour from 8 AM to 6 PM:

```
0 8-18/2 * * *
```

## Timezone Handling

Each blackout schedule includes a `timezone` field that specifies the timezone
for the cron expression. The timezone uses IANA format such as `America/New_York`
or `Europe/London`. When no timezone is specified, the alerter uses UTC.

The alerter converts the current time to the specified timezone before
evaluating the cron expression. This approach ensures that scheduled blackouts
trigger at the correct local time regardless of the server's system timezone.

## Common Examples

The following examples demonstrate common scheduling patterns:

### Daily Schedules

This expression triggers daily at midnight:

```
0 0 * * *
```

This expression triggers daily at 3:30 AM:

```
30 3 * * *
```

### Hourly Schedules

This expression triggers at the start of every hour:

```
0 * * * *
```

This expression triggers every 15 minutes:

```
*/15 * * * *
```

This expression triggers every 5 minutes:

```
*/5 * * * *
```

### Weekly Schedules

This expression triggers every Sunday at 2:00 AM:

```
0 2 * * 0
```

This expression triggers every weekday (Monday through Friday) at 6:00 AM:

```
0 6 * * 1-5
```

This expression triggers every weekend (Saturday and Sunday) at midnight:

```
0 0 * * 0,6
```

### Monthly Schedules

This expression triggers at midnight on the first day of each month:

```
0 0 1 * *
```

This expression triggers at 4:30 AM on the 1st and 15th of each month:

```
30 4 1,15 * *
```

### Business Hours

This expression triggers every hour from 9 AM to 5 PM on weekdays:

```
0 9-17 * * 1-5
```

This expression triggers every 30 minutes during business hours on weekdays:

```
0,30 9-17 * * 1-5
```

## Blackout Schedule Configuration

Blackout schedules are stored in the datastore and can be created through
the API or database. Each schedule includes the following fields:

| Field | Description |
|-------|-------------|
| `name` | A descriptive name for the schedule |
| `cron_expression` | The 5-field cron expression |
| `duration_minutes` | How long the blackout lasts |
| `timezone` | IANA timezone for the schedule |
| `reason` | Explanation for the blackout |
| `enabled` | Whether the schedule is active |
| `connection_id` | Specific connection (null for all) |
| `database_name` | Specific database (null for all) |

In the following example, a blackout schedule is configured for Sunday
maintenance in the Eastern timezone:

```yaml
name: sunday_maintenance
cron_expression: "0 2 * * 0"
duration_minutes: 120
timezone: America/New_York
reason: Weekly maintenance window
enabled: true
```

This configuration creates a 2-hour blackout starting at 2:00 AM Eastern
time every Sunday.

## Validation

The alerter validates cron expressions when blackout schedules are created
or updated. Invalid expressions result in an error. Common validation errors
include:

- Invalid field values (e.g., minute value 60).
- Incorrect number of fields (must be exactly 5).
- Invalid syntax (e.g., unmatched ranges).

You can test cron expressions before deploying by checking the next trigger
time using online cron expression tools or the alerter debug logs.
