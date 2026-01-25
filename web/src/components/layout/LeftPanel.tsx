import { type ReactNode } from 'react';
import { useUI } from '../../context/UIContext';

interface Props {
    children: ReactNode;
}

export default function LeftPanel({ children }: Props) {
    const { state, dispatch } = useUI();

    if (!state.panels.left) {
        return (
            <button
                className="panel-toggle panel-toggle-left"
                onClick={() => dispatch({ type: 'TOGGLE_PANEL', panel: 'left' })}
                title="Show left panel"
            >
                &raquo;
            </button>
        );
    }

    return (
        <aside className="sidebar">
            <div className="panel-header">
                <button
                    className="panel-close"
                    onClick={() => dispatch({ type: 'TOGGLE_PANEL', panel: 'left' })}
                    title="Hide panel"
                >
                    &laquo;
                </button>
            </div>
            {children}
        </aside>
    );
}
