/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { useState, MouseEvent } from 'react';

export interface UseMenuReturn {
    anchorEl: HTMLElement | null;
    open: boolean;
    handleOpen: (event: MouseEvent<HTMLElement>) => void;
    handleClose: () => void;
}

export const useMenu = (): UseMenuReturn => {
    const [anchorEl, setAnchorEl] = useState<HTMLElement | null>(null);

    const handleOpen = (event: MouseEvent<HTMLElement>): void => {
        setAnchorEl(event.currentTarget);
    };

    const handleClose = (): void => {
        setAnchorEl(null);
    };

    return {
        anchorEl,
        open: Boolean(anchorEl),
        handleOpen,
        handleClose,
    };
};
