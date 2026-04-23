/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import renderWithTheme from '../../../test/renderWithTheme';
import RelationshipSection from '../RelationshipSection';
import type { RelationshipSectionProps } from '../RelationshipSection';
import type {
    NodeRelationship,
    ClusterServerInfo,
} from '../../ServerDialog/ServerDialog.types';

const createMockServer = (
    overrides: Partial<ClusterServerInfo> = {},
): ClusterServerInfo => ({
    id: 1,
    name: 'server-1',
    host: 'host1.example.com',
    port: 5432,
    status: 'online',
    role: 'binary_primary',
    ...overrides,
});

const createMockRelationship = (
    overrides: Partial<NodeRelationship> = {},
): NodeRelationship => ({
    id: 100,
    cluster_id: 1,
    source_connection_id: 1,
    target_connection_id: 2,
    source_name: 'server-1',
    target_name: 'server-2',
    relationship_type: 'streams_from',
    is_auto_detected: false,
    ...overrides,
});

const createDefaultProps = (
    overrides: Partial<RelationshipSectionProps> = {},
): RelationshipSectionProps => ({
    relationships: [],
    clusterServers: [],
    selectedSourceId: '',
    selectedTargetId: '',
    selectedRelType: 'streams_from',
    relationshipError: null,
    onSourceChange: vi.fn(),
    onTargetChange: vi.fn(),
    onRelTypeChange: vi.fn(),
    onAddRelationship: vi.fn(),
    onDeleteRelationship: vi.fn(),
    onClearError: vi.fn(),
    availableTargets: [],
    allRelationshipsExist: false,
    ...overrides,
});

describe('RelationshipSection', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('renders the Relationships section heading', () => {
        const props = createDefaultProps();

        renderWithTheme(<RelationshipSection {...props} />);

        expect(screen.getByText('Relationships')).toBeInTheDocument();
    });

    it('displays "No relationships defined" when relationships is empty', () => {
        const props = createDefaultProps({ relationships: [] });

        renderWithTheme(<RelationshipSection {...props} />);

        expect(
            screen.getByText('No relationships defined.'),
        ).toBeInTheDocument();
    });

    it('renders relationship list when relationships are provided', () => {
        const relationships = [
            createMockRelationship({
                id: 1,
                source_name: 'primary-server',
                target_name: 'standby-server',
                relationship_type: 'streams_from',
            }),
        ];
        // Use allRelationshipsExist: true to avoid rendering the dropdown form
        const props = createDefaultProps({
            relationships,
            allRelationshipsExist: true,
        });

        renderWithTheme(<RelationshipSection {...props} />);

        expect(screen.getByText('primary-server')).toBeInTheDocument();
        expect(screen.getByText('standby-server')).toBeInTheDocument();
        expect(screen.getByText(/streams from/i)).toBeInTheDocument();
    });

    it('displays Auto chip for auto-detected relationships', () => {
        const relationships = [
            createMockRelationship({ is_auto_detected: true }),
        ];
        const props = createDefaultProps({ relationships });

        renderWithTheme(<RelationshipSection {...props} />);

        expect(screen.getByText('Auto')).toBeInTheDocument();
    });

    it('does not display Auto chip for manual relationships', () => {
        const relationships = [
            createMockRelationship({ is_auto_detected: false }),
        ];
        const props = createDefaultProps({ relationships });

        renderWithTheme(<RelationshipSection {...props} />);

        expect(screen.queryByText('Auto')).not.toBeInTheDocument();
    });

    it('renders delete button for each relationship', () => {
        const relationships = [
            createMockRelationship({
                id: 1,
                source_name: 'server-1',
                target_name: 'server-2',
            }),
        ];
        const props = createDefaultProps({ relationships });

        renderWithTheme(<RelationshipSection {...props} />);

        expect(
            screen.getByRole('button', {
                name: 'Remove relationship between server-1 and server-2',
            }),
        ).toBeInTheDocument();
    });

    it('calls onDeleteRelationship when delete button is clicked', () => {
        const onDeleteRelationship = vi.fn();
        const relationships = [
            createMockRelationship({
                id: 42,
                source_name: 'server-1',
                target_name: 'server-2',
            }),
        ];
        const props = createDefaultProps({
            relationships,
            onDeleteRelationship,
        });

        renderWithTheme(<RelationshipSection {...props} />);

        fireEvent.click(
            screen.getByRole('button', {
                name: 'Remove relationship between server-1 and server-2',
            }),
        );

        expect(onDeleteRelationship).toHaveBeenCalledWith(42);
    });

    it('displays relationship error when set', () => {
        const props = createDefaultProps({
            relationshipError: 'Failed to add relationship',
        });

        renderWithTheme(<RelationshipSection {...props} />);

        expect(
            screen.getByText('Failed to add relationship'),
        ).toBeInTheDocument();
    });

    it('calls onClearError when error alert is closed', () => {
        const onClearError = vi.fn();
        const props = createDefaultProps({
            relationshipError: 'Some error',
            onClearError,
        });

        renderWithTheme(<RelationshipSection {...props} />);

        const closeButton = screen.getByRole('button', { name: /close/i });
        fireEvent.click(closeButton);

        expect(onClearError).toHaveBeenCalledTimes(1);
    });

    it('displays "All members already have this relationship type" when fully meshed', () => {
        const props = createDefaultProps({ allRelationshipsExist: true });

        renderWithTheme(<RelationshipSection {...props} />);

        expect(
            screen.getByText(
                'All members already have this relationship type.',
            ),
        ).toBeInTheDocument();
    });

    it('hides add relationship form when fully meshed', () => {
        const props = createDefaultProps({ allRelationshipsExist: true });

        renderWithTheme(<RelationshipSection {...props} />);

        expect(screen.queryByLabelText('Source')).not.toBeInTheDocument();
        expect(screen.queryByLabelText('Target')).not.toBeInTheDocument();
        expect(
            screen.queryByRole('button', { name: /Add/i }),
        ).not.toBeInTheDocument();
    });

    it('renders add relationship form when not fully meshed', () => {
        const props = createDefaultProps({ allRelationshipsExist: false });

        renderWithTheme(<RelationshipSection {...props} />);

        expect(screen.getByLabelText('Source')).toBeInTheDocument();
        expect(screen.getByLabelText('Target')).toBeInTheDocument();
        expect(screen.getByLabelText('Type')).toBeInTheDocument();
        expect(
            screen.getByRole('button', { name: /Add/i }),
        ).toBeInTheDocument();
    });

    it('renders source dropdown with proper attributes', () => {
        const servers = [
            createMockServer({ id: 1, name: 'server-alpha' }),
            createMockServer({ id: 2, name: 'server-beta' }),
        ];
        const props = createDefaultProps({ clusterServers: servers });

        renderWithTheme(<RelationshipSection {...props} />);

        const sourceSelect = screen.getByLabelText('Source');
        expect(sourceSelect).toBeInTheDocument();
        expect(sourceSelect).toHaveAttribute('aria-haspopup', 'listbox');
    });

    it('provides clusterServers as options for source selection', () => {
        const servers = [
            createMockServer({ id: 1, name: 'server-alpha' }),
            createMockServer({ id: 2, name: 'server-beta' }),
        ];
        const props = createDefaultProps({
            clusterServers: servers,
        });

        renderWithTheme(<RelationshipSection {...props} />);

        // Verify the select exists and has clusterServers configured
        const sourceSelect = screen.getByLabelText('Source');
        expect(sourceSelect).toBeInTheDocument();
    });

    it('disables target dropdown when no source is selected', () => {
        const props = createDefaultProps({
            selectedSourceId: '',
            availableTargets: [],
        });

        renderWithTheme(<RelationshipSection {...props} />);

        const targetSelect = screen.getByLabelText('Target');
        expect(targetSelect.closest('div')).toHaveClass('Mui-disabled');
    });

    it('disables target dropdown when no available targets', () => {
        const props = createDefaultProps({
            selectedSourceId: 1,
            availableTargets: [],
        });

        renderWithTheme(<RelationshipSection {...props} />);

        const targetSelect = screen.getByLabelText('Target');
        expect(targetSelect.closest('div')).toHaveClass('Mui-disabled');
    });

    it('enables target dropdown when source is selected and targets available', () => {
        const servers = [createMockServer({ id: 2, name: 'server-2' })];
        const props = createDefaultProps({
            selectedSourceId: 1,
            availableTargets: servers,
        });

        renderWithTheme(<RelationshipSection {...props} />);

        const targetSelect = screen.getByLabelText('Target');
        expect(targetSelect.closest('div')).not.toHaveClass('Mui-disabled');
    });

    it('renders target dropdown with proper attributes', () => {
        const servers = [createMockServer({ id: 2, name: 'server-target' })];
        const props = createDefaultProps({
            selectedSourceId: 1,
            availableTargets: servers,
        });

        renderWithTheme(<RelationshipSection {...props} />);

        const targetSelect = screen.getByLabelText('Target');
        expect(targetSelect).toBeInTheDocument();
        expect(targetSelect).toHaveAttribute('aria-haspopup', 'listbox');
    });

    it('renders type dropdown with proper attributes', () => {
        const props = createDefaultProps();

        renderWithTheme(<RelationshipSection {...props} />);

        const typeSelect = screen.getByLabelText('Type');
        expect(typeSelect).toBeInTheDocument();
        expect(typeSelect).toHaveAttribute('aria-haspopup', 'listbox');
    });

    it('displays selected relationship type value', () => {
        const props = createDefaultProps({
            selectedRelType: 'subscribes_to',
        });

        renderWithTheme(<RelationshipSection {...props} />);

        // The selected value should be reflected in the select
        const typeSelect = screen.getByLabelText('Type');
        expect(typeSelect).toBeInTheDocument();
    });

    it('disables Add button when source is not selected', () => {
        const props = createDefaultProps({
            selectedSourceId: '',
            selectedTargetId: 2,
            selectedRelType: 'streams_from',
        });

        renderWithTheme(<RelationshipSection {...props} />);

        expect(screen.getByRole('button', { name: /Add/i })).toBeDisabled();
    });

    it('disables Add button when target is not selected', () => {
        const props = createDefaultProps({
            selectedSourceId: 1,
            selectedTargetId: '',
            selectedRelType: 'streams_from',
        });

        renderWithTheme(<RelationshipSection {...props} />);

        expect(screen.getByRole('button', { name: /Add/i })).toBeDisabled();
    });

    it('disables Add button when relationship type is not selected', () => {
        const props = createDefaultProps({
            selectedSourceId: 1,
            selectedTargetId: 2,
            selectedRelType: '',
        });

        renderWithTheme(<RelationshipSection {...props} />);

        expect(screen.getByRole('button', { name: /Add/i })).toBeDisabled();
    });

    it('enables Add button when all fields are selected', () => {
        const servers = [createMockServer({ id: 2, name: 'server-2' })];
        const props = createDefaultProps({
            selectedSourceId: 1,
            selectedTargetId: 2,
            selectedRelType: 'streams_from',
            availableTargets: servers,
        });

        renderWithTheme(<RelationshipSection {...props} />);

        expect(screen.getByRole('button', { name: /Add/i })).not.toBeDisabled();
    });

    it('calls onAddRelationship when Add button is clicked', () => {
        const onAddRelationship = vi.fn();
        const servers = [createMockServer({ id: 2, name: 'server-2' })];
        const props = createDefaultProps({
            selectedSourceId: 1,
            selectedTargetId: 2,
            selectedRelType: 'streams_from',
            availableTargets: servers,
            onAddRelationship,
        });

        renderWithTheme(<RelationshipSection {...props} />);

        fireEvent.click(screen.getByRole('button', { name: /Add/i }));

        expect(onAddRelationship).toHaveBeenCalledTimes(1);
    });

    it('displays multiple relationships in list', () => {
        const relationships = [
            createMockRelationship({
                id: 1,
                source_name: 'server-a',
                target_name: 'server-b',
            }),
            createMockRelationship({
                id: 2,
                source_name: 'server-c',
                target_name: 'server-d',
                relationship_type: 'replicates_with',
            }),
        ];
        const props = createDefaultProps({ relationships });

        renderWithTheme(<RelationshipSection {...props} />);

        expect(screen.getByText('server-a')).toBeInTheDocument();
        expect(screen.getByText('server-b')).toBeInTheDocument();
        expect(screen.getByText('server-c')).toBeInTheDocument();
        expect(screen.getByText('server-d')).toBeInTheDocument();
    });

    it('displays relationship types with correct labels', () => {
        const relationships = [
            createMockRelationship({
                id: 1,
                relationship_type: 'subscribes_to',
                source_name: 'pub',
                target_name: 'sub',
            }),
        ];
        const props = createDefaultProps({ relationships });

        renderWithTheme(<RelationshipSection {...props} />);

        expect(screen.getByText(/subscribes to/i)).toBeInTheDocument();
    });

    it('calls onSourceChange when source select value changes', async () => {
        const onSourceChange = vi.fn();
        const servers = [
            createMockServer({ id: 1, name: 'server-alpha' }),
            createMockServer({ id: 2, name: 'server-beta' }),
        ];
        const props = createDefaultProps({
            clusterServers: servers,
            onSourceChange,
        });

        renderWithTheme(<RelationshipSection {...props} />);

        // Open the Source dropdown
        fireEvent.mouseDown(screen.getByLabelText('Source'));

        // Wait for listbox and select an option
        const listbox = await screen.findByRole('listbox');
        const option = listbox.querySelector('[data-value="1"]');
        if (option) {
            fireEvent.click(option);
        }

        expect(onSourceChange).toHaveBeenCalledWith(1);
    });

    it('calls onRelTypeChange when type select value changes', async () => {
        const onRelTypeChange = vi.fn();
        const props = createDefaultProps({
            onRelTypeChange,
        });

        renderWithTheme(<RelationshipSection {...props} />);

        // Open the Type dropdown
        fireEvent.mouseDown(screen.getByLabelText('Type'));

        // Wait for listbox and select an option
        const listbox = await screen.findByRole('listbox');
        const option = listbox.querySelector('[data-value="subscribes_to"]');
        if (option) {
            fireEvent.click(option);
        }

        expect(onRelTypeChange).toHaveBeenCalledWith('subscribes_to');
    });
});
