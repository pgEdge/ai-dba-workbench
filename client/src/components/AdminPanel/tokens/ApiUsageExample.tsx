/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
import { Box, Typography } from '@mui/material';
import { alpha, useTheme } from '@mui/material/styles';
import { subsectionLabelSx } from '../styles';

const API_EXAMPLES = `# List connections
curl -s -H "Authorization: Bearer <token>" \\
  <server-url>/api/v1/connections

# Get connection details
curl -s -H "Authorization: Bearer <token>" \\
  <server-url>/api/v1/connections/1

# Create a connection
curl -s -X POST -H "Authorization: Bearer <token>" \\
  -H "Content-Type: application/json" \\
  -d @connection.json \\
  <server-url>/api/v1/connections

# Delete a connection
curl -s -X DELETE -H "Authorization: Bearer <token>" \\
  <server-url>/api/v1/connections/1

# Chat with the AI assistant
curl -s -X POST -H "Authorization: Bearer <token>" \\
  -H "Content-Type: application/json" \\
  -d '{"messages": [{"role": "user", "content": "What tables exist in the database?"}]}' \\
  <server-url>/api/v1/llm/chat`;

/**
 * Component displaying example API usage with tokens.
 */
const ApiUsageExample: React.FC = () => {
    const theme = useTheme();

    return (
        <Box sx={{ mt: 3 }}>
            <Typography variant="subtitle2" sx={{ ...subsectionLabelSx, mb: 1 }}>
                API Usage Examples
            </Typography>
            <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>
                Use tokens with the Authorization header to access the API.
            </Typography>
            <Box
                component="pre"
                sx={{
                    bgcolor: alpha(theme.palette.text.primary, 0.08),
                    border: '1px solid',
                    borderColor: theme.palette.divider,
                    borderRadius: 1,
                    p: 2,
                    fontSize: '0.8rem',
                    fontFamily: 'monospace',
                    overflowX: 'auto',
                    whiteSpace: 'pre',
                    lineHeight: 1.6,
                    color: 'text.secondary',
                }}
            >
                {API_EXAMPLES}
            </Box>
        </Box>
    );
};

export default ApiUsageExample;
