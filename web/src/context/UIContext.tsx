import { createContext, useContext, useReducer, type ReactNode } from 'react';
import type { PanelState, RightPanelView, LeftPanelView, ViewMode } from '../types';

interface UIState {
    panels: PanelState;
    rightPanelView: RightPanelView;
    leftPanelView: LeftPanelView;
    viewMode: ViewMode;
    selectedNodeId: string | null;
    selectedEventId: string | null;
}

type UIAction =
    | { type: 'TOGGLE_PANEL'; panel: keyof PanelState }
    | { type: 'SET_RIGHT_VIEW'; view: RightPanelView }
    | { type: 'SET_LEFT_VIEW'; view: LeftPanelView }
    | { type: 'SET_VIEW_MODE'; mode: ViewMode }
    | { type: 'SELECT_NODE'; nodeId: string | null }
    | { type: 'SELECT_EVENT'; eventId: string | null }
    | { type: 'CLOSE_RIGHT_PANEL' };

const initialState: UIState = {
    panels: { left: true, right: false, bottom: true },
    rightPanelView: 'inspector',
    leftPanelView: 'workflows',
    viewMode: 'workflow',
    selectedNodeId: null,
    selectedEventId: null,
};

function uiReducer(state: UIState, action: UIAction): UIState {
    switch (action.type) {
        case 'TOGGLE_PANEL':
            return {
                ...state,
                panels: { ...state.panels, [action.panel]: !state.panels[action.panel] },
            };
        case 'SET_RIGHT_VIEW':
            return { ...state, rightPanelView: action.view, panels: { ...state.panels, right: true } };
        case 'SET_LEFT_VIEW':
            return { ...state, leftPanelView: action.view };
        case 'SET_VIEW_MODE':
            // When switching to marketplace, show right panel for event details
            // When switching to workflow, hide right panel unless a node is selected
            return {
                ...state,
                viewMode: action.mode,
                panels: {
                    ...state.panels,
                    right: action.mode === 'marketplace' ? true : state.selectedNodeId !== null,
                    bottom: action.mode === 'workflow' ? state.panels.bottom : false,
                },
                selectedNodeId: action.mode === 'marketplace' ? null : state.selectedNodeId,
            };
        case 'SELECT_NODE':
            return {
                ...state,
                selectedNodeId: action.nodeId,
                rightPanelView: action.nodeId ? 'inspector' : state.rightPanelView,
                panels: { ...state.panels, right: action.nodeId !== null },
            };
        case 'SELECT_EVENT':
            return { ...state, selectedEventId: action.eventId };
        case 'CLOSE_RIGHT_PANEL':
            return {
                ...state,
                panels: { ...state.panels, right: false },
                selectedNodeId: null,
            };
        default:
            return state;
    }
}

const UIContext = createContext<{
    state: UIState;
    dispatch: React.Dispatch<UIAction>;
} | null>(null);

export function UIProvider({ children }: { children: ReactNode }) {
    const [state, dispatch] = useReducer(uiReducer, initialState);

    return (
        <UIContext.Provider value={{ state, dispatch }}>
            {children}
        </UIContext.Provider>
    );
}

export function useUI() {
    const context = useContext(UIContext);
    if (!context) {
        throw new Error('useUI must be used within a UIProvider');
    }
    return context;
}
