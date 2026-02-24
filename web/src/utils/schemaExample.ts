/**
 * Generates an example payload from a JSON Schema.
 * Recursively traverses the schema structure to create representative values.
 */
export function schemaToExample(schema: Record<string, unknown> | undefined): unknown {
    if (!schema) {
        return {};
    }

    const type = schema.type as string | undefined;

    // Handle specific types
    switch (type) {
        case 'object':
            return handleObject(schema);
        case 'array':
            return handleArray(schema);
        case 'string':
            return handleString(schema);
        case 'number':
        case 'integer':
            return handleNumber(schema);
        case 'boolean':
            return handleBoolean(schema);
        case 'null':
            return null;
        default:
            // Try to infer from other properties
            if (schema.properties) {
                return handleObject(schema);
            }
            if (schema.items) {
                return handleArray(schema);
            }
            return {};
    }
}

function handleObject(schema: Record<string, unknown>): Record<string, unknown> {
    const result: Record<string, unknown> = {};
    const properties = schema.properties as Record<string, Record<string, unknown>> | undefined;

    if (properties) {
        for (const [key, propSchema] of Object.entries(properties)) {
            result[key] = schemaToExample(propSchema);
        }
    }

    return result;
}

function handleArray(schema: Record<string, unknown>): unknown[] {
    const items = schema.items as Record<string, unknown> | undefined;
    if (items) {
        return [schemaToExample(items)];
    }
    return [];
}

function handleString(schema: Record<string, unknown>): string {
    // Check for format hints
    const format = schema.format as string | undefined;
    const example = schema.example as string | undefined;
    const defaultValue = schema.default as string | undefined;

    if (example) return example;
    if (defaultValue) return defaultValue;

    switch (format) {
        case 'date':
            return '2024-01-15';
        case 'date-time':
            return '2024-01-15T10:30:00Z';
        case 'time':
            return '10:30:00';
        case 'email':
            return 'user@example.com';
        case 'uri':
        case 'url':
            return 'https://example.com';
        case 'uuid':
            return '550e8400-e29b-41d4-a716-446655440000';
        default:
            return 'string';
    }
}

function handleNumber(schema: Record<string, unknown>): number {
    const example = schema.example as number | undefined;
    const defaultValue = schema.default as number | undefined;
    const minimum = schema.minimum as number | undefined;
    const maximum = schema.maximum as number | undefined;

    if (example !== undefined) return example;
    if (defaultValue !== undefined) return defaultValue;

    // Generate reasonable default
    if (minimum !== undefined && maximum !== undefined) {
        return Math.floor((minimum + maximum) / 2);
    }
    if (minimum !== undefined) return minimum;
    if (maximum !== undefined) return maximum;

    return 0;
}

function handleBoolean(schema: Record<string, unknown>): boolean {
    const example = schema.example as boolean | undefined;
    const defaultValue = schema.default as boolean | undefined;

    if (example !== undefined) return example;
    if (defaultValue !== undefined) return defaultValue;

    return true;
}
