/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

# Material-UI Usage Patterns and Customizations

This document provides comprehensive guidance on using Material-UI (MUI) in
the pgEdge AI DBA Workbench frontend, including theming, component patterns, and
customization strategies.

## Theme Configuration

### Base Theme Setup

```typescript
// src/styles/theme.ts
import { createTheme, ThemeOptions } from '@mui/material/styles';

// Custom color palette
const colors = {
    primary: {
        main: '#1976d2',
        light: '#42a5f5',
        dark: '#1565c0',
        contrastText: '#fff',
    },
    secondary: {
        main: '#dc004e',
        light: '#f73378',
        dark: '#9a0036',
        contrastText: '#fff',
    },
    success: {
        main: '#2e7d32',
        light: '#4caf50',
        dark: '#1b5e20',
        contrastText: '#fff',
    },
    error: {
        main: '#d32f2f',
        light: '#ef5350',
        dark: '#c62828',
        contrastText: '#fff',
    },
    warning: {
        main: '#ed6c02',
        light: '#ff9800',
        dark: '#e65100',
        contrastText: '#fff',
    },
    info: {
        main: '#0288d1',
        light: '#03a9f4',
        dark: '#01579b',
        contrastText: '#fff',
    },
};

// Common theme options
const commonTheme: ThemeOptions = {
    typography: {
        fontFamily: [
            '-apple-system',
            'BlinkMacSystemFont',
            '"Segoe UI"',
            'Roboto',
            '"Helvetica Neue"',
            'Arial',
            'sans-serif',
        ].join(','),
        h1: {
            fontSize: '2.5rem',
            fontWeight: 600,
        },
        h2: {
            fontSize: '2rem',
            fontWeight: 600,
        },
        h3: {
            fontSize: '1.75rem',
            fontWeight: 600,
        },
        h4: {
            fontSize: '1.5rem',
            fontWeight: 600,
        },
        h5: {
            fontSize: '1.25rem',
            fontWeight: 600,
        },
        h6: {
            fontSize: '1rem',
            fontWeight: 600,
        },
        button: {
            textTransform: 'none', // Disable uppercase buttons
        },
    },
    shape: {
        borderRadius: 8,
    },
    spacing: 8, // Base spacing unit (default is 8px)
    components: {
        MuiButton: {
            styleOverrides: {
                root: {
                    borderRadius: 8,
                    padding: '8px 16px',
                },
                sizeLarge: {
                    padding: '12px 24px',
                },
                sizeSmall: {
                    padding: '4px 12px',
                },
            },
            defaultProps: {
                disableElevation: true, // Flat buttons by default
            },
        },
        MuiCard: {
            styleOverrides: {
                root: {
                    borderRadius: 12,
                },
            },
        },
        MuiTextField: {
            defaultProps: {
                variant: 'outlined',
            },
        },
        MuiPaper: {
            styleOverrides: {
                root: {
                    borderRadius: 12,
                },
            },
        },
    },
};

// Light theme
export const lightTheme: ThemeOptions = {
    ...commonTheme,
    palette: {
        mode: 'light',
        ...colors,
        background: {
            default: '#f5f5f5',
            paper: '#ffffff',
        },
        text: {
            primary: 'rgba(0, 0, 0, 0.87)',
            secondary: 'rgba(0, 0, 0, 0.6)',
            disabled: 'rgba(0, 0, 0, 0.38)',
        },
        divider: 'rgba(0, 0, 0, 0.12)',
    },
};

// Dark theme
export const darkTheme: ThemeOptions = {
    ...commonTheme,
    palette: {
        mode: 'dark',
        ...colors,
        background: {
            default: '#121212',
            paper: '#1e1e1e',
        },
        text: {
            primary: '#ffffff',
            secondary: 'rgba(255, 255, 255, 0.7)',
            disabled: 'rgba(255, 255, 255, 0.5)',
        },
        divider: 'rgba(255, 255, 255, 0.12)',
    },
};
```

### Using Theme in Components

```typescript
// Access theme via useTheme hook
import { useTheme } from '@mui/material/styles';

const MyComponent: React.FC = () => {
    const theme = useTheme();

    return (
        <Box
            sx={{
                backgroundColor: theme.palette.background.paper,
                padding: theme.spacing(2),
                borderRadius: theme.shape.borderRadius,
            }}
        >
            Content
        </Box>
    );
};

// Or use the sx prop with theme-aware values
const MyComponent: React.FC = () => {
    return (
        <Box
            sx={{
                bgcolor: 'background.paper',
                p: 2,
                borderRadius: 1,
                color: 'text.primary',
            }}
        >
            Content
        </Box>
    );
};
```

## Layout Patterns

### App Layout with Drawer

```typescript
// src/components/layout/AppLayout.tsx
import React, { useState } from 'react';
import {
    Box,
    AppBar,
    Toolbar,
    IconButton,
    Typography,
    Drawer,
    List,
    ListItem,
    ListItemButton,
    ListItemIcon,
    ListItemText,
    Divider,
} from '@mui/material';
import {
    Menu as MenuIcon,
    Dashboard,
    Storage,
    Insights,
    Settings,
} from '@mui/icons-material';
import { useNavigate, Outlet } from 'react-router-dom';

const drawerWidth = 240;

const menuItems = [
    { text: 'Dashboard', icon: <Dashboard />, path: '/dashboard' },
    { text: 'Connections', icon: <Storage />, path: '/connections' },
    { text: 'Monitoring', icon: <Insights />, path: '/monitoring' },
    { text: 'Settings', icon: <Settings />, path: '/settings' },
];

export const AppLayout: React.FC = () => {
    const [mobileOpen, setMobileOpen] = useState(false);
    const navigate = useNavigate();

    const handleDrawerToggle = () => {
        setMobileOpen(!mobileOpen);
    };

    const drawer = (
        <Box>
            <Toolbar>
                <Typography variant="h6" noWrap component="div">
                    pgEdge AI
                </Typography>
            </Toolbar>
            <Divider />
            <List>
                {menuItems.map((item) => (
                    <ListItem key={item.text} disablePadding>
                        <ListItemButton onClick={() => navigate(item.path)}>
                            <ListItemIcon>{item.icon}</ListItemIcon>
                            <ListItemText primary={item.text} />
                        </ListItemButton>
                    </ListItem>
                ))}
            </List>
        </Box>
    );

    return (
        <Box sx={{ display: 'flex' }}>
            <AppBar
                position="fixed"
                sx={{
                    width: { sm: `calc(100% - ${drawerWidth}px)` },
                    ml: { sm: `${drawerWidth}px` },
                }}
            >
                <Toolbar>
                    <IconButton
                        color="inherit"
                        edge="start"
                        onClick={handleDrawerToggle}
                        sx={{ mr: 2, display: { sm: 'none' } }}
                    >
                        <MenuIcon />
                    </IconButton>
                    <Typography variant="h6" noWrap component="div">
                        pgEdge AI DBA Workbench
                    </Typography>
                </Toolbar>
            </AppBar>

            <Box
                component="nav"
                sx={{ width: { sm: drawerWidth }, flexShrink: { sm: 0 } }}
            >
                {/* Mobile drawer */}
                <Drawer
                    variant="temporary"
                    open={mobileOpen}
                    onClose={handleDrawerToggle}
                    ModalProps={{ keepMounted: true }}
                    sx={{
                        display: { xs: 'block', sm: 'none' },
                        '& .MuiDrawer-paper': {
                            boxSizing: 'border-box',
                            width: drawerWidth,
                        },
                    }}
                >
                    {drawer}
                </Drawer>

                {/* Desktop drawer */}
                <Drawer
                    variant="permanent"
                    sx={{
                        display: { xs: 'none', sm: 'block' },
                        '& .MuiDrawer-paper': {
                            boxSizing: 'border-box',
                            width: drawerWidth,
                        },
                    }}
                    open
                >
                    {drawer}
                </Drawer>
            </Box>

            <Box
                component="main"
                sx={{
                    flexGrow: 1,
                    p: 3,
                    width: { sm: `calc(100% - ${drawerWidth}px)` },
                }}
            >
                <Toolbar /> {/* Spacer for fixed AppBar */}
                <Outlet /> {/* Child routes render here */}
            </Box>
        </Box>
    );
};
```

### Responsive Grid Layout

```typescript
// src/pages/DashboardPage.tsx
import { Grid, Card, CardContent, Typography } from '@mui/material';

export const DashboardPage: React.FC = () => {
    return (
        <Grid container spacing={3}>
            {/* Full width header */}
            <Grid item xs={12}>
                <Typography variant="h4">Dashboard</Typography>
            </Grid>

            {/* Responsive cards */}
            <Grid item xs={12} sm={6} md={4}>
                <Card>
                    <CardContent>
                        <Typography variant="h6">Connections</Typography>
                        <Typography variant="h3">12</Typography>
                    </CardContent>
                </Card>
            </Grid>

            <Grid item xs={12} sm={6} md={4}>
                <Card>
                    <CardContent>
                        <Typography variant="h6">Active Queries</Typography>
                        <Typography variant="h3">45</Typography>
                    </CardContent>
                </Card>
            </Grid>

            <Grid item xs={12} sm={6} md={4}>
                <Card>
                    <CardContent>
                        <Typography variant="h6">Alerts</Typography>
                        <Typography variant="h3">3</Typography>
                    </CardContent>
                </Card>
            </Grid>

            {/* Full width chart */}
            <Grid item xs={12}>
                <Card>
                    <CardContent>
                        <Typography variant="h6">Metrics</Typography>
                        {/* Chart component */}
                    </CardContent>
                </Card>
            </Grid>
        </Grid>
    );
};
```

## Common Component Patterns

### Data Table with Actions

```typescript
// src/components/features/ConnectionTable.tsx
import React, { useState } from 'react';
import {
    Table,
    TableBody,
    TableCell,
    TableContainer,
    TableHead,
    TableRow,
    Paper,
    IconButton,
    Chip,
    Menu,
    MenuItem,
} from '@mui/material';
import { MoreVert, FiberManualRecord } from '@mui/icons-material';
import type { Connection } from '../../types/models';

interface ConnectionTableProps {
    connections: Connection[];
    onEdit: (connection: Connection) => void;
    onDelete: (connection: Connection) => void;
}

export const ConnectionTable: React.FC<ConnectionTableProps> = ({
    connections,
    onEdit,
    onDelete,
}) => {
    const [anchorEl, setAnchorEl] = useState<null | HTMLElement>(null);
    const [selectedConnection, setSelectedConnection] =
        useState<Connection | null>(null);

    const handleMenuOpen = (
        event: React.MouseEvent<HTMLElement>,
        connection: Connection
    ) => {
        setAnchorEl(event.currentTarget);
        setSelectedConnection(connection);
    };

    const handleMenuClose = () => {
        setAnchorEl(null);
        setSelectedConnection(null);
    };

    const handleEdit = () => {
        if (selectedConnection) {
            onEdit(selectedConnection);
        }
        handleMenuClose();
    };

    const handleDelete = () => {
        if (selectedConnection) {
            onDelete(selectedConnection);
        }
        handleMenuClose();
    };

    return (
        <TableContainer component={Paper}>
            <Table>
                <TableHead>
                    <TableRow>
                        <TableCell>Status</TableCell>
                        <TableCell>Name</TableCell>
                        <TableCell>Host</TableCell>
                        <TableCell>Database</TableCell>
                        <TableCell>Type</TableCell>
                        <TableCell align="right">Actions</TableCell>
                    </TableRow>
                </TableHead>
                <TableBody>
                    {connections.map((connection) => (
                        <TableRow key={connection.id}>
                            <TableCell>
                                <FiberManualRecord
                                    sx={{
                                        color: connection.isActive
                                            ? 'success.main'
                                            : 'error.main',
                                        fontSize: 12,
                                    }}
                                />
                            </TableCell>
                            <TableCell>{connection.name}</TableCell>
                            <TableCell>{connection.host}</TableCell>
                            <TableCell>{connection.database}</TableCell>
                            <TableCell>
                                <Chip
                                    label={connection.type}
                                    size="small"
                                    color={
                                        connection.type === 'production'
                                            ? 'error'
                                            : 'default'
                                    }
                                />
                            </TableCell>
                            <TableCell align="right">
                                <IconButton
                                    onClick={(e) => handleMenuOpen(e, connection)}
                                >
                                    <MoreVert />
                                </IconButton>
                            </TableCell>
                        </TableRow>
                    ))}
                </TableBody>
            </Table>

            <Menu
                anchorEl={anchorEl}
                open={Boolean(anchorEl)}
                onClose={handleMenuClose}
            >
                <MenuItem onClick={handleEdit}>Edit</MenuItem>
                <MenuItem onClick={handleDelete}>Delete</MenuItem>
            </Menu>
        </TableContainer>
    );
};
```

### Form with Validation

```typescript
// src/components/features/ConnectionForm.tsx
import React, { useState } from 'react';
import {
    Dialog,
    DialogTitle,
    DialogContent,
    DialogActions,
    TextField,
    Button,
    FormControl,
    InputLabel,
    Select,
    MenuItem,
    Alert,
} from '@mui/material';
import type { CreateConnectionInput } from '../../types/models';

interface ConnectionFormProps {
    open: boolean;
    onClose: () => void;
    onSubmit: (data: CreateConnectionInput) => Promise<void>;
}

export const ConnectionForm: React.FC<ConnectionFormProps> = ({
    open,
    onClose,
    onSubmit,
}) => {
    const [formData, setFormData] = useState<CreateConnectionInput>({
        name: '',
        host: '',
        port: 5432,
        database: '',
        username: '',
        password: '',
        sslMode: 'prefer',
    });

    const [errors, setErrors] = useState<Record<string, string>>({});
    const [isSubmitting, setIsSubmitting] = useState(false);
    const [submitError, setSubmitError] = useState<string | null>(null);

    const validate = (): boolean => {
        const newErrors: Record<string, string> = {};

        if (!formData.name.trim()) {
            newErrors.name = 'Name is required';
        }

        if (!formData.host.trim()) {
            newErrors.host = 'Host is required';
        }

        if (formData.port < 1 || formData.port > 65535) {
            newErrors.port = 'Port must be between 1 and 65535';
        }

        if (!formData.database.trim()) {
            newErrors.database = 'Database is required';
        }

        if (!formData.username.trim()) {
            newErrors.username = 'Username is required';
        }

        setErrors(newErrors);
        return Object.keys(newErrors).length === 0;
    };

    const handleChange = (field: keyof CreateConnectionInput, value: any) => {
        setFormData((prev) => ({ ...prev, [field]: value }));
        // Clear error for this field when user types
        if (errors[field]) {
            setErrors((prev) => {
                const newErrors = { ...prev };
                delete newErrors[field];
                return newErrors;
            });
        }
    };

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        setSubmitError(null);

        if (!validate()) {
            return;
        }

        setIsSubmitting(true);
        try {
            await onSubmit(formData);
            onClose();
            // Reset form
            setFormData({
                name: '',
                host: '',
                port: 5432,
                database: '',
                username: '',
                password: '',
                sslMode: 'prefer',
            });
        } catch (error) {
            setSubmitError(
                error instanceof Error ? error.message : 'Failed to create connection'
            );
        } finally {
            setIsSubmitting(false);
        }
    };

    return (
        <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
            <form onSubmit={handleSubmit}>
                <DialogTitle>Create Connection</DialogTitle>
                <DialogContent>
                    {submitError && (
                        <Alert severity="error" sx={{ mb: 2 }}>
                            {submitError}
                        </Alert>
                    )}

                    <TextField
                        autoFocus
                        margin="dense"
                        label="Connection Name"
                        fullWidth
                        value={formData.name}
                        onChange={(e) => handleChange('name', e.target.value)}
                        error={!!errors.name}
                        helperText={errors.name}
                        required
                    />

                    <TextField
                        margin="dense"
                        label="Host"
                        fullWidth
                        value={formData.host}
                        onChange={(e) => handleChange('host', e.target.value)}
                        error={!!errors.host}
                        helperText={errors.host}
                        required
                    />

                    <TextField
                        margin="dense"
                        label="Port"
                        type="number"
                        fullWidth
                        value={formData.port}
                        onChange={(e) =>
                            handleChange('port', parseInt(e.target.value, 10))
                        }
                        error={!!errors.port}
                        helperText={errors.port}
                        required
                    />

                    <TextField
                        margin="dense"
                        label="Database"
                        fullWidth
                        value={formData.database}
                        onChange={(e) => handleChange('database', e.target.value)}
                        error={!!errors.database}
                        helperText={errors.database}
                        required
                    />

                    <TextField
                        margin="dense"
                        label="Username"
                        fullWidth
                        value={formData.username}
                        onChange={(e) => handleChange('username', e.target.value)}
                        error={!!errors.username}
                        helperText={errors.username}
                        required
                    />

                    <TextField
                        margin="dense"
                        label="Password"
                        type="password"
                        fullWidth
                        value={formData.password}
                        onChange={(e) => handleChange('password', e.target.value)}
                    />

                    <FormControl fullWidth margin="dense">
                        <InputLabel>SSL Mode</InputLabel>
                        <Select
                            value={formData.sslMode}
                            label="SSL Mode"
                            onChange={(e) => handleChange('sslMode', e.target.value)}
                        >
                            <MenuItem value="disable">Disable</MenuItem>
                            <MenuItem value="prefer">Prefer</MenuItem>
                            <MenuItem value="require">Require</MenuItem>
                            <MenuItem value="verify-ca">Verify CA</MenuItem>
                            <MenuItem value="verify-full">Verify Full</MenuItem>
                        </Select>
                    </FormControl>
                </DialogContent>
                <DialogActions>
                    <Button onClick={onClose} disabled={isSubmitting}>
                        Cancel
                    </Button>
                    <Button
                        type="submit"
                        variant="contained"
                        disabled={isSubmitting}
                    >
                        {isSubmitting ? 'Creating...' : 'Create'}
                    </Button>
                </DialogActions>
            </form>
        </Dialog>
    );
};
```

### Confirmation Dialog

```typescript
// src/components/common/ConfirmDialog.tsx
import React from 'react';
import {
    Dialog,
    DialogTitle,
    DialogContent,
    DialogContentText,
    DialogActions,
    Button,
} from '@mui/material';

interface ConfirmDialogProps {
    open: boolean;
    title: string;
    message: string;
    confirmText?: string;
    cancelText?: string;
    confirmColor?: 'primary' | 'secondary' | 'error' | 'warning';
    onConfirm: () => void;
    onCancel: () => void;
}

export const ConfirmDialog: React.FC<ConfirmDialogProps> = ({
    open,
    title,
    message,
    confirmText = 'Confirm',
    cancelText = 'Cancel',
    confirmColor = 'primary',
    onConfirm,
    onCancel,
}) => {
    return (
        <Dialog open={open} onClose={onCancel}>
            <DialogTitle>{title}</DialogTitle>
            <DialogContent>
                <DialogContentText>{message}</DialogContentText>
            </DialogContent>
            <DialogActions>
                <Button onClick={onCancel}>{cancelText}</Button>
                <Button
                    onClick={onConfirm}
                    variant="contained"
                    color={confirmColor}
                    autoFocus
                >
                    {confirmText}
                </Button>
            </DialogActions>
        </Dialog>
    );
};

// Usage
const [confirmOpen, setConfirmOpen] = useState(false);

<ConfirmDialog
    open={confirmOpen}
    title="Delete Connection"
    message="Are you sure you want to delete this connection? This action cannot be undone."
    confirmText="Delete"
    confirmColor="error"
    onConfirm={() => {
        handleDelete();
        setConfirmOpen(false);
    }}
    onCancel={() => setConfirmOpen(false)}
/>
```

### Loading States

```typescript
// src/components/common/LoadingState.tsx
import React from 'react';
import { Box, CircularProgress, Typography, Skeleton } from '@mui/material';

// Full page loading
export const PageLoading: React.FC = () => {
    return (
        <Box
            display="flex"
            justifyContent="center"
            alignItems="center"
            minHeight="400px"
        >
            <Box textAlign="center">
                <CircularProgress />
                <Typography variant="body2" color="text.secondary" sx={{ mt: 2 }}>
                    Loading...
                </Typography>
            </Box>
        </Box>
    );
};

// Inline loading
export const InlineLoading: React.FC = () => {
    return (
        <Box display="flex" alignItems="center" gap={1}>
            <CircularProgress size={20} />
            <Typography variant="body2" color="text.secondary">
                Loading...
            </Typography>
        </Box>
    );
};

// Skeleton loading for cards
export const CardSkeleton: React.FC = () => {
    return (
        <Box sx={{ p: 2 }}>
            <Skeleton variant="text" width="60%" height={32} />
            <Skeleton variant="text" width="40%" height={24} sx={{ mt: 1 }} />
            <Skeleton variant="rectangular" height={200} sx={{ mt: 2 }} />
        </Box>
    );
};

// Table skeleton
export const TableSkeleton: React.FC<{ rows?: number }> = ({ rows = 5 }) => {
    return (
        <Box>
            {Array.from({ length: rows }).map((_, index) => (
                <Skeleton
                    key={index}
                    variant="rectangular"
                    height={52}
                    sx={{ mb: 1 }}
                />
            ))}
        </Box>
    );
};
```

### Empty States

```typescript
// src/components/common/EmptyState.tsx
import React from 'react';
import { Box, Typography, Button } from '@mui/material';
import { Inbox } from '@mui/icons-material';

interface EmptyStateProps {
    icon?: React.ReactNode;
    title: string;
    description?: string;
    actionLabel?: string;
    onAction?: () => void;
}

export const EmptyState: React.FC<EmptyStateProps> = ({
    icon = <Inbox />,
    title,
    description,
    actionLabel,
    onAction,
}) => {
    return (
        <Box
            display="flex"
            flexDirection="column"
            alignItems="center"
            justifyContent="center"
            minHeight="400px"
            textAlign="center"
            p={4}
        >
            <Box
                sx={{
                    fontSize: 64,
                    color: 'text.disabled',
                    mb: 2,
                }}
            >
                {icon}
            </Box>
            <Typography variant="h6" color="text.primary" gutterBottom>
                {title}
            </Typography>
            {description && (
                <Typography variant="body2" color="text.secondary" paragraph>
                    {description}
                </Typography>
            )}
            {actionLabel && onAction && (
                <Button variant="contained" onClick={onAction} sx={{ mt: 2 }}>
                    {actionLabel}
                </Button>
            )}
        </Box>
    );
};

// Usage
<EmptyState
    title="No connections yet"
    description="Get started by creating your first database connection"
    actionLabel="Create Connection"
    onAction={() => setFormOpen(true)}
/>
```

## Responsive Design Patterns

### Breakpoint Usage

```typescript
import { useMediaQuery, useTheme } from '@mui/material';

const MyComponent: React.FC = () => {
    const theme = useTheme();
    const isMobile = useMediaQuery(theme.breakpoints.down('sm'));
    const isTablet = useMediaQuery(theme.breakpoints.between('sm', 'md'));
    const isDesktop = useMediaQuery(theme.breakpoints.up('md'));

    return (
        <Box>
            {isMobile && <MobileView />}
            {isTablet && <TabletView />}
            {isDesktop && <DesktopView />}
        </Box>
    );
};
```

### Responsive Props

```typescript
// Responsive spacing
<Box
    sx={{
        p: { xs: 2, sm: 3, md: 4 }, // padding
        m: { xs: 1, sm: 2, md: 3 }, // margin
    }}
>
    Content
</Box>

// Responsive display
<Box
    sx={{
        display: { xs: 'none', md: 'block' } // Hidden on mobile, visible on desktop
    }}
>
    Desktop only content
</Box>

// Responsive grid
<Grid
    container
    spacing={{ xs: 2, md: 3 }}
    columns={{ xs: 4, sm: 8, md: 12 }}
>
    <Grid item xs={4} sm={4} md={3}>
        Card
    </Grid>
</Grid>
```

## Accessibility Patterns

### Form Accessibility

```typescript
<TextField
    label="Username"
    inputProps={{
        'aria-label': 'username',
        'aria-required': 'true',
        'aria-describedby': 'username-helper-text',
    }}
    helperText={
        <span id="username-helper-text">
            Enter your username
        </span>
    }
/>
```

### Button Accessibility

```typescript
<IconButton
    aria-label="delete connection"
    onClick={handleDelete}
>
    <Delete />
</IconButton>

<Button
    aria-label="Create new connection"
    aria-describedby="create-connection-description"
>
    Create
</Button>
<Typography id="create-connection-description" className="sr-only">
    Opens a dialog to create a new database connection
</Typography>
```

### Skip Links

```typescript
// src/components/layout/SkipLink.tsx
import { Link } from '@mui/material';

export const SkipLink: React.FC = () => {
    return (
        <Link
            href="#main-content"
            sx={{
                position: 'absolute',
                left: '-9999px',
                zIndex: 999,
                '&:focus': {
                    left: '50%',
                    top: '8px',
                    transform: 'translateX(-50%)',
                    backgroundColor: 'primary.main',
                    color: 'primary.contrastText',
                    padding: '8px 16px',
                    borderRadius: 1,
                },
            }}
        >
            Skip to main content
        </Link>
    );
};
```

## Custom Component Variants

### Creating Custom Variants

```typescript
// src/styles/theme.ts
import { createTheme } from '@mui/material/styles';

declare module '@mui/material/Button' {
    interface ButtonPropsVariantOverrides {
        dashed: true;
    }
}

export const theme = createTheme({
    components: {
        MuiButton: {
            variants: [
                {
                    props: { variant: 'dashed' },
                    style: {
                        border: '2px dashed',
                        borderColor: '#1976d2',
                    },
                },
            ],
        },
    },
});

// Usage
<Button variant="dashed">Dashed Button</Button>
```

## Performance Optimization

### Virtual Scrolling for Large Lists

```typescript
// Install react-window: npm install react-window
import { FixedSizeList } from 'react-window';
import { ListItem, ListItemText } from '@mui/material';

const VirtualizedList: React.FC<{ items: string[] }> = ({ items }) => {
    const Row = ({ index, style }: { index: number; style: React.CSSProperties }) => (
        <ListItem style={style} component="div">
            <ListItemText primary={items[index]} />
        </ListItem>
    );

    return (
        <FixedSizeList
            height={400}
            width="100%"
            itemSize={46}
            itemCount={items.length}
            overscanCount={5}
        >
            {Row}
        </FixedSizeList>
    );
};
```

## Best Practices Summary

1. **Use the sx prop** for styling instead of makeStyles or styled components
2. **Leverage theme values** for consistent spacing, colors, and typography
3. **Implement responsive design** using breakpoints and responsive props
4. **Ensure accessibility** with proper ARIA attributes and keyboard navigation
5. **Create reusable components** by extracting common patterns
6. **Use MUI's built-in components** whenever possible before creating custom ones
7. **Customize through theme** rather than per-component overrides
8. **Test responsive behavior** across different screen sizes
9. **Follow Material Design guidelines** for consistent UX
10. **Optimize performance** with virtualization for large datasets
