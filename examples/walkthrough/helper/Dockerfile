#--------------------------------------------------------------------------
#
# pgEdge AI DBA Workbench
#
# Copyright (c) 2025 - 2026, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#--------------------------------------------------------------------------
FROM python:3.12-alpine

RUN apk add --no-cache docker-cli

WORKDIR /app
COPY server.py .

EXPOSE 8090
CMD ["python3", "server.py"]
