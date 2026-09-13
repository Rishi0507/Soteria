import type { Meta, StoryObj } from '@storybook/react-vite'
import { AllergenPanel } from './AllergenPanel'
import { useProductAllergens } from './useProductAllergens'

const meta: Meta<typeof AllergenPanel> = {
    title: 'Storefront/AllergenPanel',
    component: AllergenPanel,
}
export default meta

type Story = StoryObj<typeof AllergenPanel>

const base = {
    gtin: '03017620422003',
    source: 'OPEN_FOOD_FACTS' as const,
    fetched_at: '2026-09-12T13:46:32.555Z',
}

export const CompleteWithAllergens: Story = {
    args: {
        data: {
            ...base,
            product_name: 'Nutella',
            brand: 'Nutella, Ferrero',
            coverage: 'COMPLETE',
            allergens: ['en:milk', 'en:nuts', 'en:soybeans'],
            traces: [],
            ingredients_text: 'Sugar, palm oil, hazelnuts 13%, skimmed milk powder, cocoa',
        },
    },
}

export const CompleteNoAllergens: Story = {
    args: {
        data: {
            ...base,
            product_name: 'Spring Water',
            coverage: 'COMPLETE',
            allergens: [],
            traces: [],
            ingredients_text: 'Water',
        },
    },
}

export const WithTraces: Story = {
    args: {
        data: {
            ...base,
            product_name: 'Dark Chocolate',
            coverage: 'COMPLETE',
            allergens: ['en:soybeans'],
            traces: ['en:milk', 'en:nuts'],
            ingredients_text: 'Cocoa mass, sugar, cocoa butter, soy lecithin',
        },
    },
}

export const Partial: Story = {
    args: {
        data: {
            ...base,
            product_name: 'Unknown Brand Crackers',
            coverage: 'PARTIAL',
            allergens: [],
            ingredients_text: 'Wheat flour, vegetable oil, salt',
        },
    },
}

export const Absent: Story = {
    args: {
        data: {
            ...base,
            coverage: 'ABSENT',
            allergens: [],
        },
    },
}

export const Loading: Story = {
    args: { data: null, loading: true },
}
function LivePanel({ gtin }: { gtin: string }) {
    const { data, loading } = useProductAllergens(gtin)
    return <AllergenPanel data={data} loading={loading} />
}

export const LiveComplete: Story = {
    render: () => <LivePanel gtin="3017620422003" />,
}

export const LivePartial: Story = {
    render: () => <LivePanel gtin="1111111111111" />,
}

export const LiveAbsent: Story = {
    render: () => <LivePanel gtin="0000000000000" />,
}