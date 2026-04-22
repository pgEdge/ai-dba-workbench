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
import {
    ListItemButton,
    ListItemIcon,
    ListItemText,
} from '@mui/material';
import { ChevronRight as ChevronIcon } from '@mui/icons-material';
import { SvgIconComponent } from '@mui/icons-material';
import {
    styles,
    getNavItemSx,
    getNavItemIconColor,
    getNavItemLabelProps,
} from '../helpPanelStyles';

export interface HelpNavItemProps {
    icon: SvgIconComponent;
    label: string;
    pageId: string;
    currentPage: string;
    onClick: (pageId: string) => void;
}

/**
 * HelpNavItem - Navigation item in the help sidebar
 */
const HelpNavItem: React.FC<HelpNavItemProps> = ({
    icon: Icon,
    label,
    pageId,
    currentPage,
    onClick,
}) => {
    const isActive = currentPage === pageId;

    return (
        <ListItemButton
            onClick={() => onClick(pageId)}
            sx={getNavItemSx(isActive)}
        >
            <ListItemIcon sx={styles.navItemIcon}>
                <Icon
                    sx={{
                        ...styles.navItemIconSize,
                        color: getNavItemIconColor(isActive),
                    }}
                />
            </ListItemIcon>
            <ListItemText
                primary={label}
                primaryTypographyProps={getNavItemLabelProps(isActive)}
            />
            {isActive && (
                <ChevronIcon sx={{
                    ...styles.chevronActive,
                    color: 'primary.main',
                }} />
            )}
        </ListItemButton>
    );
};

export default HelpNavItem;
