import { type ReactNode } from 'react';
import { useUI } from '../../context/UIContext';

interface Props {
    children: ReactNode;
}

export default function RightPanel({ children }: Props) {
    const { state, dispatch } = useUI();

    if (!state.panels.right) {
        return null;
    }

    return (
        <aside className="right-panel">
            <div className="panel-header">
                <button
                    className="panel-close"
                    onClick={() => dispatch({ type: 'CLOSE_RIGHT_PANEL' })}
                    title="Close panel"
                >
                    &times;
                </button>
            </div>
            {children}
        </aside>
    );
}
