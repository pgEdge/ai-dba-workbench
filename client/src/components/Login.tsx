/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Login Page
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 * Styled to match pgEdge Cloud product aesthetics
 *
 *-------------------------------------------------------------------------
 */

import React, { useState, useEffect } from 'react';
import {
    Box,
    Card,
    CardContent,
    TextField,
    Button,
    Typography,
    Alert,
    Container,
    keyframes,
    alpha,
} from '@mui/material';
import { Theme } from '@mui/material/styles';
import { useAuth } from '../contexts/AuthContext';
import logoLight from '../assets/images/logo-light.png';

// Subtle floating animation for decorative elements
const float = keyframes`
  0%, 100% { transform: translateY(0px); }
  50% { transform: translateY(-15px); }
`;

const pulse = keyframes`
  0%, 100% { opacity: 0.4; transform: scale(1); }
  50% { opacity: 0.6; transform: scale(1.05); }
`;

// Wave ripple animation - noticeable horizontal movement
const waveRipple = keyframes`
  0% { transform: translateX(0) scale(1.15); }
  50% { transform: translateX(-5%) scale(1.2); }
  100% { transform: translateX(0) scale(1.15); }
`;

// Secondary wave with different timing - moves opposite direction
const waveRipple2 = keyframes`
  0% { transform: translateX(0) scale(1.2); }
  50% { transform: translateX(4%) scale(1.15); }
  100% { transform: translateX(0) scale(1.2); }
`;

// Shimmer effect for visible light play
const shimmer = keyframes`
  0% { opacity: 0.1; }
  50% { opacity: 0.4; }
  100% { opacity: 0.1; }
`;

// --- Style constants (Issue 23) ---

const pageContainerSx = {
    minHeight: '100vh',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    position: 'relative',
    overflow: 'hidden',
    backgroundColor: 'background.default',
};

const waveBaseSx = {
    position: 'absolute',
    backgroundImage: 'url(https://a.storyblok.com/f/187930/1200x560/7852cd29b7/home-page-hero-bg-1200.jpg)',
    backgroundSize: 'cover',
    backgroundPosition: 'center',
    backgroundRepeat: 'no-repeat',
    zIndex: 0,
};

const primaryWaveSx = {
    ...waveBaseSx,
    top: '-15%',
    left: '-10%',
    right: '-10%',
    bottom: '-15%',
    animation: `${waveRipple} 12s ease-in-out infinite`,
};

const secondaryWaveSx = {
    ...waveBaseSx,
    top: '-20%',
    left: '-15%',
    right: '-15%',
    bottom: '-20%',
    opacity: 0.4,
    animation: `${waveRipple2} 15s ease-in-out infinite`,
    animationDelay: '-3s',
};

const gradientOverlaySx = {
    position: 'absolute',
    top: 0,
    left: 0,
    right: 0,
    bottom: 0,
    background: 'linear-gradient(135deg, rgba(15, 23, 42, 0.75) 0%, rgba(30, 41, 59, 0.65) 50%, rgba(15, 23, 42, 0.8) 100%)',
    zIndex: 1,
};

const getShimmerSx = (theme: Theme) => ({
    position: 'absolute',
    top: '10%',
    left: '20%',
    width: '60%',
    height: '80%',
    background: `radial-gradient(ellipse at center, ${alpha(theme.palette.primary.light, 0.35)} 0%, transparent 60%)`,
    animation: `${shimmer} 6s ease-in-out infinite`,
    zIndex: 1,
});

const getGlowOrb1Sx = (theme: Theme) => ({
    position: 'absolute',
    top: '5%',
    left: '0%',
    width: 400,
    height: 400,
    borderRadius: '50%',
    background: `radial-gradient(circle, ${alpha(theme.palette.primary.main, 0.25)} 0%, transparent 60%)`,
    filter: 'blur(30px)',
    animation: `${pulse} 8s ease-in-out infinite`,
    zIndex: 1,
});

const getGlowOrb2Sx = (theme: Theme) => ({
    position: 'absolute',
    bottom: '5%',
    right: '0%',
    width: 450,
    height: 450,
    borderRadius: '50%',
    background: `radial-gradient(circle, ${alpha(theme.palette.info.main, 0.2)} 0%, transparent 60%)`,
    filter: 'blur(35px)',
    animation: `${pulse} 10s ease-in-out infinite`,
    animationDelay: '2s',
    zIndex: 1,
});

const getFloatingSquareSx = (theme: Theme) => ({
    position: 'absolute',
    top: '12%',
    right: '12%',
    width: 100,
    height: 100,
    border: `2px solid ${alpha(theme.palette.primary.main, 0.35)}`,
    borderRadius: '20px',
    transform: 'rotate(15deg)',
    animation: `${float} 6s ease-in-out infinite`,
    zIndex: 2,
});

const getFloatingCircleSx = (theme: Theme) => ({
    position: 'absolute',
    bottom: '18%',
    left: '8%',
    width: 70,
    height: 70,
    border: `2px solid ${alpha(theme.palette.primary.light, 0.3)}`,
    borderRadius: '50%',
    animation: `${float} 8s ease-in-out infinite`,
    animationDelay: '1s',
    zIndex: 2,
});

const getFloatingDiamond1Sx = (theme: Theme) => ({
    position: 'absolute',
    top: '50%',
    right: '6%',
    width: 50,
    height: 50,
    backgroundColor: alpha(theme.palette.primary.main, 0.2),
    borderRadius: '12px',
    transform: 'rotate(45deg)',
    animation: `${float} 7s ease-in-out infinite`,
    animationDelay: '3s',
    zIndex: 2,
});

const getFloatingDiamond2Sx = (theme: Theme) => ({
    position: 'absolute',
    top: '25%',
    left: '6%',
    width: 40,
    height: 40,
    backgroundColor: alpha(theme.palette.primary.light, 0.15),
    borderRadius: '10px',
    animation: `${float} 5s ease-in-out infinite`,
    animationDelay: '0.5s',
    zIndex: 2,
});

const getFloatingSmallSquareSx = (theme: Theme) => ({
    position: 'absolute',
    bottom: '35%',
    right: '18%',
    width: 30,
    height: 30,
    border: `2px solid ${alpha(theme.palette.primary.main, 0.25)}`,
    borderRadius: '8px',
    transform: 'rotate(30deg)',
    animation: `${float} 9s ease-in-out infinite`,
    animationDelay: '2s',
    zIndex: 2,
});

const cardSx = {
    backdropFilter: 'blur(20px)',
    backgroundColor: 'rgba(255, 255, 255, 0.98)',
    borderRadius: 3,
    border: '1px solid rgba(255, 255, 255, 0.2)',
    overflow: 'visible',
    boxShadow: '0 25px 50px -12px rgba(0, 0, 0, 0.25)',
};

const cardContentSx = {
    p: { xs: 3, sm: 5 },
};

const logoContainerSx = {
    textAlign: 'center',
    mb: 4,
};

const logoSx = {
    height: '48px',
    mb: 2,
};

const titleSx = {
    fontWeight: 600,
    color: 'text.primary',
    mb: 0.5,
};

const subtitleSx = {
    color: 'text.secondary',
};

const alertSx = {
    mb: 3,
    borderRadius: 1,
};

const textFieldSx = {
    '& .MuiOutlinedInput-root': {
        borderRadius: 1,
        '&:hover .MuiOutlinedInput-notchedOutline': {
            borderColor: 'grey.400',
        },
        '&.Mui-focused .MuiOutlinedInput-notchedOutline': {
            borderColor: 'primary.main',
            borderWidth: 2,
        },
    },
    '& .MuiInputLabel-root.Mui-focused': {
        color: 'primary.main',
    },
};

const getSubmitButtonSx = (theme: Theme) => ({
    mt: 3,
    py: 1.5,
    borderRadius: 1,
    fontWeight: 600,
    fontSize: '1rem',
    textTransform: 'none',
    background: theme.palette.primary.main,
    boxShadow: '0 4px 14px 0 rgba(14, 165, 233, 0.39)',
    '&:hover': {
        background: theme.palette.primary.dark,
        boxShadow: '0 6px 20px 0 rgba(14, 165, 233, 0.5)',
    },
    '&.Mui-disabled': {
        background: theme.palette.grey[200],
        color: theme.palette.grey[400],
    },
});

const footerCaptionSx = {
    color: 'grey.400',
};

const copyrightSx = {
    display: 'block',
    textAlign: 'center',
    mt: 3,
    color: 'rgba(255, 255, 255, 0.6)',
};

// --- Component ---

const Login = () => {
    const [username, setUsername] = useState('');
    const [password, setPassword] = useState('');
    const [error, setError] = useState('');
    const [warning, setWarning] = useState('');
    const [loading, setLoading] = useState(false);
    const { login } = useAuth();

    // Check for disconnect message on mount
    useEffect(() => {
        const disconnectMsg = sessionStorage.getItem('disconnectMessage');
        if (disconnectMsg) {
            setWarning(disconnectMsg);
            sessionStorage.removeItem('disconnectMessage');
        }
    }, []);

    const handleSubmit = async (e: React.FormEvent<HTMLFormElement>) => {
        e.preventDefault();
        setError('');
        setWarning('');
        setLoading(true);

        try {
            await login(username, password);
        } catch (err: unknown) {
            setError(err instanceof Error ? err.message : 'Failed to login');
        } finally {
            setLoading(false);
        }
    };

    return (
        <Box sx={pageContainerSx}>
            {/* Animated wave background - primary layer */}
            <Box sx={primaryWaveSx} />

            {/* Secondary wave layer - moves opposite direction for depth */}
            <Box sx={secondaryWaveSx} />

            {/* Gradient overlay for depth */}
            <Box sx={gradientOverlaySx} />

            {/* Shimmer light effect */}
            <Box sx={getShimmerSx} />

            {/* Decorative glow orbs */}
            <Box sx={getGlowOrb1Sx} />
            <Box sx={getGlowOrb2Sx} />

            {/* Floating decorative elements */}
            <Box sx={getFloatingSquareSx} />
            <Box sx={getFloatingCircleSx} />
            <Box sx={getFloatingDiamond1Sx} />
            <Box sx={getFloatingDiamond2Sx} />
            <Box sx={getFloatingSmallSquareSx} />

            <Container maxWidth="sm" sx={{ position: 'relative', zIndex: 3 }}>
                <Card elevation={24} sx={cardSx}>
                    <CardContent sx={cardContentSx}>
                        <Box sx={logoContainerSx}>
                            <Box
                                component="img"
                                src={logoLight}
                                alt="pgEdge"
                                sx={logoSx}
                            />
                            <Typography
                                variant="h5"
                                component="h1"
                                sx={titleSx}
                            >
                                AI DBA Workbench
                            </Typography>
                            <Typography variant="body2" sx={subtitleSx}>
                                Sign in to continue
                            </Typography>
                        </Box>

                        {warning && (
                            <Alert
                                severity="warning"
                                sx={alertSx}
                                onClose={() => setWarning('')}
                            >
                                {warning}
                            </Alert>
                        )}

                        {error && (
                            <Alert
                                severity="error"
                                sx={alertSx}
                            >
                                {error}
                            </Alert>
                        )}

                        <form onSubmit={handleSubmit} noValidate>
                            <TextField
                                fullWidth
                                label="Username"
                                type="text"
                                name="username"
                                id="username"
                                value={username}
                                onChange={(e) => setUsername(e.target.value)}
                                margin="normal"
                                required
                                autoFocus
                                disabled={loading}
                                inputProps={{
                                    autoComplete: 'off',
                                }}
                                sx={textFieldSx}
                            />

                            <TextField
                                fullWidth
                                label="Password"
                                type="password"
                                name="password"
                                value={password}
                                onChange={(e) => setPassword(e.target.value)}
                                margin="normal"
                                required
                                disabled={loading}
                                inputProps={{
                                    autoComplete: 'current-password',
                                }}
                                sx={textFieldSx}
                            />

                            <Button
                                fullWidth
                                type="submit"
                                variant="contained"
                                size="large"
                                disabled={loading}
                                sx={getSubmitButtonSx}
                            >
                                {loading ? 'Signing in...' : 'Sign In'}
                            </Button>
                        </form>

                        <Box sx={{ mt: 3, textAlign: 'center' }}>
                            <Typography variant="caption" sx={footerCaptionSx}>
                                Contact your administrator to create an account
                            </Typography>
                        </Box>
                    </CardContent>
                </Card>

                {/* Copyright footer */}
                <Typography
                    variant="caption"
                    sx={copyrightSx}
                >
                    &copy; 2025 - 2026, pgEdge, Inc.
                </Typography>
            </Container>
        </Box>
    );
};

export default Login;
