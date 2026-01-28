import { useMemo } from 'react';
import { useMarketplace } from '../../context/MarketplaceContext';
import { schemaToExample } from '../../utils/schemaExample';
import './EventDetail.css';

export function EventDetail() {
    const { selectedEvent, loading } = useMarketplace();

    const examplePayload = useMemo(() => {
        if (!selectedEvent?.schema) {
            return null;
        }
        return schemaToExample(selectedEvent.schema as Record<string, unknown>);
    }, [selectedEvent?.schema]);

    if (loading) {
        return (
            <div className="event-detail loading">
                <div className="spinner" />
                <span>Loading event details...</span>
            </div>
        );
    }

    if (!selectedEvent) {
        return (
            <div className="event-detail empty">
                <span className="empty-icon">👈</span>
                <span>Select an event from the list to view its details</span>
            </div>
        );
    }

    return (
        <div className="event-detail">
            <header className="event-header">
                <span className="event-badge">Event</span>
                <h2 className="event-name">{selectedEvent.name}</h2>
                <span className="event-domain">@{selectedEvent.domain}</span>
            </header>

            <section className="event-section">
                <h3>Description</h3>
                <p className="description">
                    {selectedEvent.description || 'No description available'}
                </p>
            </section>

            <section className="event-section metadata">
                <h3>Metadata</h3>
                <dl className="metadata-list">
                    <div className="metadata-item">
                        <dt>Full Name</dt>
                        <dd>
                            <code>{selectedEvent.full_name}</code>
                        </dd>
                    </div>
                    {selectedEvent.owner && (
                        <div className="metadata-item">
                            <dt>Owner</dt>
                            <dd>{selectedEvent.owner}</dd>
                        </div>
                    )}
                    <div className="metadata-item">
                        <dt>NATS Subject</dt>
                        <dd>
                            <code>marketplace.{selectedEvent.domain}.{selectedEvent.name}</code>
                        </dd>
                    </div>
                </dl>
            </section>

            {selectedEvent.schema && (
                <section className="event-section">
                    <h3>Schema</h3>
                    <pre className="code-block">
                        <code>{JSON.stringify(selectedEvent.schema, null, 2)}</code>
                    </pre>
                </section>
            )}

            {examplePayload !== null && (
                <section className="event-section">
                    <h3>Example Payload</h3>
                    <pre className="code-block example">
                        <code>{JSON.stringify(examplePayload, null, 2)}</code>
                    </pre>
                </section>
            )}
        </div>
    );
}
