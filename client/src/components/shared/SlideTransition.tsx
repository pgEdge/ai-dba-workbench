/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React from 'react';
import { Slide } from '@mui/material';
import { TransitionProps } from '@mui/material/transitions';

/**
 * Shared slide-up transition used by full-screen dialogs throughout
 * the application. Wraps MUI's Slide component with direction="up".
 */
const SlideTransition = React.forwardRef(function SlideTransition(
    props: TransitionProps & { children: React.ReactElement },
    ref: React.Ref<unknown>,
) {
    return <Slide direction="up" ref={ref} {...props} />;
});

export default SlideTransition;
