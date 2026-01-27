/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

/**
 * Recursively collect all roles from servers and their children
 */
export const collectAllRoles = (servers) => {
    if (!servers || servers.length === 0) return [];
    const roles = [];
    const traverse = (serverList) => {
        serverList.forEach(s => {
            const role = s.primary_role || s.role;
            if (role) roles.push(role);
            if (s.children?.length > 0) traverse(s.children);
        });
    };
    traverse(servers);
    return roles;
};

/**
 * Detect cluster type based on server roles (including children)
 * Returns: 'spock' | 'binary' | 'logical' | 'default'
 * Priority: spock > logical > binary (logical takes precedence since it's often combined with binary)
 */
export const getClusterType = (cluster) => {
    if (!cluster) return 'default';
    if (cluster.name?.toLowerCase().includes('spock')) return 'spock';
    if (!cluster.servers || cluster.servers.length === 0) return 'default';

    // Collect roles from all servers including nested children
    const roles = collectAllRoles(cluster.servers);
    if (roles.length === 0) return 'default';

    // Check for Spock (highest priority)
    if (roles.some(r => r === 'spock_node')) return 'spock';

    // Check for logical replication (takes precedence over binary)
    const logicalRoles = ['logical_publisher', 'logical_subscriber', 'logical_bidirectional'];
    if (roles.some(r => logicalRoles.includes(r))) return 'logical';

    // Check for binary replication
    const binaryRoles = ['binary_primary', 'binary_standby', 'binary_cascading'];
    if (roles.some(r => binaryRoles.includes(r))) return 'binary';

    return 'default';
};

/**
 * Compute effective role for display based on cluster context
 * In logical replication clusters, binary_primary should display as logical_publisher
 */
export const getEffectiveRole = (serverRole, clusterType) => {
    if (clusterType === 'logical' && serverRole === 'binary_primary') {
        return 'logical_publisher';
    }
    return serverRole;
};

/**
 * Count all servers recursively (including children)
 */
export const countServersRecursive = (servers, filterFn = () => true) => {
    if (!servers) return 0;
    return servers.reduce((count, server) => {
        const current = filterFn(server) ? 1 : 0;
        const childCount = countServersRecursive(server.children, filterFn);
        return count + current + childCount;
    }, 0);
};

/**
 * Collect all expandable server IDs recursively
 */
export const collectExpandableServerIds = (servers) => {
    if (!servers) return [];
    return servers.flatMap(server => {
        const ids = server.is_expandable || server.children?.length > 0 ? [server.id] : [];
        return [...ids, ...collectExpandableServerIds(server.children)];
    });
};

/**
 * Recursively filter servers by search query, including children
 */
export const filterServersRecursive = (servers, query) => {
    if (!servers) return [];

    return servers.reduce((result, server) => {
        const serverMatches =
            server.name.toLowerCase().includes(query) ||
            server.host?.toLowerCase().includes(query);

        // Recursively filter children
        const filteredChildren = filterServersRecursive(server.children, query);

        // Include server if it matches or has matching children
        if (serverMatches || filteredChildren.length > 0) {
            result.push({
                ...server,
                children: serverMatches ? server.children : filteredChildren,
            });
        }

        return result;
    }, []);
};

/**
 * Load a value from localStorage with JSON parsing
 */
export const loadFromStorage = (key, defaultValue) => {
    try {
        const stored = localStorage.getItem(key);
        if (stored === null) return defaultValue;
        return JSON.parse(stored);
    } catch {
        return defaultValue;
    }
};

/**
 * Save a value to localStorage with JSON serialization
 */
export const saveToStorage = (key, value) => {
    try {
        localStorage.setItem(key, JSON.stringify(value));
    } catch {
        // Ignore storage errors (quota exceeded, etc.)
    }
};

/**
 * Format relative time for display (e.g., "just now", "5m ago")
 */
export const formatRelativeTime = (date) => {
    if (!date) return '';
    const seconds = Math.floor((new Date() - date) / 1000);
    if (seconds < 60) return 'just now';
    const minutes = Math.floor(seconds / 60);
    return `${minutes}m ago`;
};
