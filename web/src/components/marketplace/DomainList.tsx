import { useMarketplace } from '../../context/MarketplaceContext';
import type { EventDefinition } from '../../types';
import './DomainList.css';

interface DomainListProps {
    onEventSelect?: (event: EventDefinition) => void;
}

export function DomainList({ onEventSelect }: DomainListProps) {
    const { domains, selectedEvent, selectEvent, loading, error } = useMarketplace();

    const handleEventClick = async (event: EventDefinition) => {
        await selectEvent(event.domain, event.name);
        onEventSelect?.(event);
    };

    if (loading && domains.length === 0) {
        return (
            <div className="domain-list loading">
                <div className="spinner" />
                <span>Loading marketplace events...</span>
            </div>
        );
    }

    if (error) {
        return (
            <div className="domain-list error">
                <span className="error-icon">⚠</span>
                <span>{error}</span>
            </div>
        );
    }

    if (domains.length === 0) {
        return (
            <div className="domain-list empty">
                <span className="empty-icon">📭</span>
                <span>No events in the marketplace yet</span>
            </div>
        );
    }

    return (
        <div className="domain-list">
            {domains.map((group) => (
                <div key={group.domain} className="domain-group">
                    <div className="domain-header">
                        <span className="domain-icon">📁</span>
                        <span className="domain-name">{group.domain}</span>
                        <span className="event-count">{group.events.length}</span>
                    </div>
                    <ul className="event-list">
                        {group.events.map((event) => (
                            <li
                                key={event.full_name}
                                className={`event-item ${selectedEvent?.full_name === event.full_name ? 'selected' : ''
                                    }`}
                                onClick={() => handleEventClick(event)}
                            >
                                <span className="event-icon">⚡</span>
                                <div className="event-info">
                                    <span className="event-name">{event.name}</span>
                                    {event.description && (
                                        <span className="event-description">
                                            {event.description}
                                        </span>
                                    )}
                                </div>
                            </li>
                        ))}
                    </ul>
                </div>
            ))}
        </div>
    );
}
