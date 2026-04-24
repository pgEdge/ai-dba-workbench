/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

/**
 * Format connection context data into a readable summary for the LLM.
 */
export function formatConnectionContext(ctx: Record<string, unknown>): string {
    const lines: string[] = [];

    const pg = ctx.postgresql as Record<string, unknown> | undefined;
    if (pg) {
        if (pg.version) {lines.push(`PostgreSQL Version: ${pg.version}`);}
        if (pg.max_connections) {lines.push(`Max Connections: ${pg.max_connections}`);}
        if (pg.installed_extensions) {
            const exts = pg.installed_extensions as string[];
            lines.push(`Installed Extensions: ${exts.join(', ')}`);
        }
        const settings = pg.settings as Record<string, string> | undefined;
        if (settings && Object.keys(settings).length > 0) {
            lines.push('Key Settings:');
            for (const [name, value] of Object.entries(settings)) {
                lines.push(`  ${name} = ${value}`);
            }
        }
    }

    const sys = ctx.system as Record<string, unknown> | undefined;
    if (sys) {
        if (sys.os_name) {lines.push(`OS: ${sys.os_name} ${sys.os_version || ''}`);}
        if (sys.architecture) {lines.push(`Architecture: ${sys.architecture}`);}

        const cpu = sys.cpu as Record<string, unknown> | undefined;
        if (cpu) {
            if (cpu.model) {lines.push(`CPU: ${cpu.model}`);}
            if (cpu.cores) {lines.push(`CPU Cores: ${cpu.cores} (${cpu.logical_processors || cpu.cores} logical)`);}
        }

        const mem = sys.memory as Record<string, unknown> | undefined;
        if (mem?.total_bytes) {
            const totalGB = ((mem.total_bytes as number) / (1024 * 1024 * 1024)).toFixed(1);
            lines.push(`Total Memory: ${totalGB} GB`);
        }

        const disks = sys.disks as Record<string, unknown>[] | undefined;
        if (disks && disks.length > 0) {
            for (const disk of disks) {
                const totalGB = ((disk.total_bytes as number) / (1024 * 1024 * 1024)).toFixed(1);
                const usedGB = ((disk.used_bytes as number) / (1024 * 1024 * 1024)).toFixed(1);
                lines.push(`Disk ${disk.mount_point}: ${usedGB}/${totalGB} GB used`);
            }
        }
    }

    return lines.length > 0 ? `\nServer Context:\n${lines.join('\n')}` : '';
}
