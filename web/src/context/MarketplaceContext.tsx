import { createContext, useContext, useState, useEffect, type ReactNode } from 'react';
import type { DomainGroup, EventDefinition } from '../types';
import { fetchMarketplaceEvents, fetchMarketplaceEvent } from '../api';

interface MarketplaceState {
    domains: DomainGroup[];
    selectedEvent: EventDefinition | null;
    loading: boolean;
    error: string | null;
}

interface MarketplaceContextValue extends MarketplaceState {
    selectEvent: (domain: string, name: string) => Promise<void>;
    refreshEvents: (domain?: string) => Promise<void>;
    clearSelectedEvent: () => void;
}

const MarketplaceContext = createContext<MarketplaceContextValue | null>(null);

export function MarketplaceProvider({ children }: { children: ReactNode }) {
    const [domains, setDomains] = useState<DomainGroup[]>([]);
    const [selectedEvent, setSelectedEvent] = useState<EventDefinition | null>(null);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState<string | null>(null);

    // Load events from marketplace
    const refreshEvents = async (domain?: string) => {
        try {
            setLoading(true);
            setError(null);
            const groups = await fetchMarketplaceEvents(domain);
            setDomains(groups);
        } catch (err) {
            setError(err instanceof Error ? err.message : 'Failed to load marketplace events');
        } finally {
            setLoading(false);
        }
    };

    // Select an event by domain and name
    const selectEvent = async (domain: string, name: string) => {
        try {
            setLoading(true);
            setError(null);
            const event = await fetchMarketplaceEvent(domain, name);
            setSelectedEvent(event);
        } catch (err) {
            setError(err instanceof Error ? err.message : 'Failed to load event details');
        } finally {
            setLoading(false);
        }
    };

    // Clear selected event
    const clearSelectedEvent = () => {
        setSelectedEvent(null);
    };

    // Initial load
    useEffect(() => {
        refreshEvents();
    }, []);

    return (
        <MarketplaceContext.Provider
            value={{
                domains,
                selectedEvent,
                loading,
                error,
                selectEvent,
                refreshEvents,
                clearSelectedEvent,
            }}
        >
            {children}
        </MarketplaceContext.Provider>
    );
}

export function useMarketplace() {
    const context = useContext(MarketplaceContext);
    if (!context) {
        throw new Error('useMarketplace must be used within a MarketplaceProvider');
    }
    return context;
}
