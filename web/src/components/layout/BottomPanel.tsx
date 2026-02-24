import { type ReactNode } from 'react';
import { useUI } from '../../context/UIContext';

interface Props {
    children: ReactNode;
}

export default function BottomPanel({ children }: Props) {
    const { state, dispatch } = useUI();

    if (!state.panels.bottom) {
        return (
            <button
                className="panel-toggle panel-toggle-bottom"
                onClick={() => dispatch({ type: 'TOGGLE_PANEL', panel: 'bottom' })}
                title="Show timeline"
            >
                Timeline &uarr;
            </button>
        );
    }

    return (
        <div className="bottom-panel">
            <div className="panel-header">
                <span>Execution Timeline</span>
                <button
                    className="panel-close"
                    onClick={() => dispatch({ type: 'TOGGLE_PANEL', panel: 'bottom' })}
                    title="Hide timeline"
                >
                    &darr;
                </button>
            </div>
            {children}
        </div>
    );
}
