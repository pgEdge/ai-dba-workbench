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
    useContext,
    useState,
    useCallback,
    useEffect,
} from 'react';

import { useAuth } from './AuthContext';
import {
    onDisconnect,
    resetConnectionHealth,
    DisconnectReason,
} from '../utils/apiClient';

interface ConnectionStatusValue {
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

    return (
        <ConnectionStatusContext.Provider
            value={{ disconnected, reason, reconnect }}
        >
            {children}
        </ConnectionStatusContext.Provider>
    );
};

export const useConnectionStatus = (): ConnectionStatusValue => {
    const context = useContext(ConnectionStatusContext);
    if (!context) {
        throw new Error(
            'useConnectionStatus must be used within a ConnectionStatusProvider',
        );
    }
    return context;
};
