/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import createSvgIcon from '@mui/material/utils/createSvgIcon';

/**
 * BlackoutIcon - a raised open-palm "stop" gesture (MUI's FrontHandSharp)
 * used throughout the blackout management UI: the status-panel header
 * control, the blackout panel banner, and the blackout management dialog.
 *
 * The icon is not exported by the pinned @mui/icons-material@5.x package;
 * it first ships in the v6 icon set, whose peer dependency requires
 * @mui/material@^6. Rather than force a major MUI upgrade for one icon,
 * we recreate it locally with MUI's public createSvgIcon utility using the
 * exact path data MUI publishes for FrontHandSharp.
 *
 * TODO: Remove this local definition and import FrontHandSharp directly
 * from @mui/icons-material once the project upgrades to MUI v6+ (where the
 * icon ships natively).
 */
const BlackoutIcon = createSvgIcon(
    <path d="M18.5 8v7H18c-1.65 0-3 1.35-3 3h-1c0-2.04 1.53-3.72 3.5-3.97V2H15v9h-1V0h-2.5v11h-1V1.5H8V12H7V4.5H4.5v11.25c0 4.56 3.69 8.25 8.25 8.25S21 20.31 21 15.75V8z" />,
    'FrontHandSharp',
);

export default BlackoutIcon;
