/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, {
    createContext,
    useState,
    useCallback,
    useEffect,
    useMemo,
} from 'react';

import { useAuth } from './useAuth';
import {
    onDisconnect,
    resetConnectionHealth,
    DisconnectReason,
} from '../utils/apiClient';

export interface ConnectionStatusValue {
    disconnected: boolean;
    reason: DisconnectReason | '';
    reconnect: () => void;
}

const ConnectionStatusContext = createContext<ConnectionStatusValue | null>(
    null,
);

export const ConnectionStatusProvider = ({
    children,
}: {
    children: React.ReactNode;
}): React.ReactElement => {
    const { forceLogout } = useAuth();
    const [disconnected, setDisconnected] = useState(false);
    const [reason, setReason] = useState<DisconnectReason | ''>('');

    useEffect(() => {
        const unsubscribe = onDisconnect((r: DisconnectReason) => {
            setDisconnected(true);
            setReason(r);
        });
        return unsubscribe;
    }, []);

    const reconnect = useCallback(() => {
        resetConnectionHealth();
        setDisconnected(false);
        setReason('');
        forceLogout();
    }, [forceLogout]);

    const value = useMemo(
        () => ({ disconnected, reason, reconnect }),
        [disconnected, reason, reconnect],
    );

    return (
        <ConnectionStatusContext.Provider value={value}>
            {children}
        </ConnectionStatusContext.Provider>
    );
};

export default ConnectionStatusContext;
