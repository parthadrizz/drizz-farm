// Curated device catalog with metadata for the Create Wizard.
// Devices not in this catalog fall back to a generic "Other" category.

export interface DeviceDef {
  id: string;
  name: string;
  category: 'phone' | 'tablet' | 'foldable' | 'tv' | 'automotive' | 'wear' | 'desktop' | 'other';
  screen: string;      // e.g., "6.3\" 1080x2400"
  density: string;     // e.g., "420dpi"
  ram?: string;        // e.g., "8GB"
  year?: number;
  popular?: boolean;   // show at top
}

// Known devices with rich metadata
const CATALOG: Record<string, Omit<DeviceDef, 'id'>> = {
  // Pixel phones
  'pixel_9_pro_xl': { name: 'Pixel 9 Pro XL', category: 'phone', screen: '6.8" 1344x2992', density: '486dpi', year: 2024, popular: true },
  'pixel_9_pro': { name: 'Pixel 9 Pro', category: 'phone', screen: '6.3" 1280x2856', density: '495dpi', year: 2024, popular: true },
  'pixel_9': { name: 'Pixel 9', category: 'phone', screen: '6.3" 1080x2424', density: '422dpi', year: 2024, popular: true },
  'pixel_8_pro': { name: 'Pixel 8 Pro', category: 'phone', screen: '6.7" 1344x2992', density: '489dpi', year: 2023, popular: true },
  'pixel_8': { name: 'Pixel 8', category: 'phone', screen: '6.2" 1080x2400', density: '428dpi', year: 2023, popular: true },
  'pixel_8a': { name: 'Pixel 8a', category: 'phone', screen: '6.1" 1080x2400', density: '430dpi', year: 2024 },
  'pixel_7_pro': { name: 'Pixel 7 Pro', category: 'phone', screen: '6.7" 1440x3120', density: '512dpi', year: 2022, popular: true },
  'pixel_7': { name: 'Pixel 7', category: 'phone', screen: '6.3" 1080x2400', density: '420dpi', year: 2022, popular: true },
  'pixel_7a': { name: 'Pixel 7a', category: 'phone', screen: '6.1" 1080x2400', density: '429dpi', year: 2023 },
  'pixel_6_pro': { name: 'Pixel 6 Pro', category: 'phone', screen: '6.7" 1440x3120', density: '512dpi', year: 2021 },
  'pixel_6': { name: 'Pixel 6', category: 'phone', screen: '6.4" 1080x2400', density: '411dpi', year: 2021 },
  'pixel_6a': { name: 'Pixel 6a', category: 'phone', screen: '6.1" 1080x2400', density: '429dpi', year: 2022 },
  'pixel_5': { name: 'Pixel 5', category: 'phone', screen: '6.0" 1080x2340', density: '432dpi', year: 2020 },
  'pixel_4': { name: 'Pixel 4', category: 'phone', screen: '5.7" 1080x2280', density: '440dpi', year: 2019 },
  'pixel_4_xl': { name: 'Pixel 4 XL', category: 'phone', screen: '6.3" 1440x3040', density: '537dpi', year: 2019 },

  // Generic
  'medium_phone': { name: 'Medium Phone', category: 'phone', screen: '6.4" 1080x2400', density: '420dpi', popular: true },
  'small_phone': { name: 'Small Phone', category: 'phone', screen: '5.4" 1080x2340', density: '475dpi' },

  // Foldables
  'pixel_fold': { name: 'Pixel Fold', category: 'foldable', screen: '7.6" 2208x1840', density: '380dpi', year: 2023, popular: true },
  'pixel_9_pro_fold': { name: 'Pixel 9 Pro Fold', category: 'foldable', screen: '8.0" 2076x2152', density: '373dpi', year: 2024, popular: true },

  // Tablets
  'pixel_tablet': { name: 'Pixel Tablet', category: 'tablet', screen: '10.95" 1600x2560', density: '276dpi', year: 2023, popular: true },
  'medium_tablet': { name: 'Medium Tablet', category: 'tablet', screen: '10.1" 1920x1200', density: '224dpi' },

  // TV
  'tv_1080p': { name: 'TV (1080p)', category: 'tv', screen: '55" 1920x1080', density: '40dpi' },
  'tv_4k': { name: 'TV (4K)', category: 'tv', screen: '55" 3840x2160', density: '80dpi' },

  // Wear
  'wearos_large_round': { name: 'Wear OS Large Round', category: 'wear', screen: '1.4" 454x454', density: '352dpi' },
  'wearos_small_round': { name: 'Wear OS Small Round', category: 'wear', screen: '1.2" 384x384', density: '352dpi' },

  // Desktop
  'desktop_medium': { name: 'Desktop Medium', category: 'desktop', screen: '14" 1920x1080', density: '160dpi' },
  'desktop_large': { name: 'Desktop Large', category: 'desktop', screen: '27" 2560x1440', density: '109dpi' },
};

const CATEGORY_ICONS: Record<string, string> = {
  phone: '📱',
  tablet: '📟',
  foldable: '📖',
  tv: '📺',
  automotive: '🚗',
  wear: '⌚',
  desktop: '🖥️',
  other: '📦',
};

const CATEGORY_ORDER = ['phone', 'foldable', 'tablet', 'tv', 'wear', 'automotive', 'desktop', 'other'];

export function enrichDevices(rawIds: string[]): DeviceDef[] {
  return rawIds.map(id => {
    const meta = CATALOG[id];
    if (meta) return { id, ...meta };

    // Guess category from name
    const lower = id.toLowerCase();
    let category: DeviceDef['category'] = 'other';
    if (lower.includes('automotive')) category = 'automotive';
    else if (lower.includes('tv') || lower.includes('television')) category = 'tv';
    else if (lower.includes('wear')) category = 'wear';
    else if (lower.includes('tablet') || lower.includes('ipad')) category = 'tablet';
    else if (lower.includes('fold')) category = 'foldable';
    else if (lower.includes('desktop')) category = 'desktop';
    else if (lower.includes('pixel') || lower.includes('galaxy') || lower.includes('phone') || lower.includes('nexus')) category = 'phone';

    return {
      id,
      name: id.replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase()),
      category,
      screen: '',
      density: '',
    };
  });
}

export function groupByCategory(devices: DeviceDef[]): { category: string; icon: string; devices: DeviceDef[] }[] {
  const groups = new Map<string, DeviceDef[]>();
  for (const d of devices) {
    const list = groups.get(d.category) || [];
    list.push(d);
    groups.set(d.category, list);
  }

  return CATEGORY_ORDER
    .filter(cat => groups.has(cat))
    .map(cat => ({
      category: cat,
      icon: CATEGORY_ICONS[cat] || '📦',
      devices: groups.get(cat)!.sort((a, b) => {
        // Popular first, then by year descending, then name
        if (a.popular && !b.popular) return -1;
        if (!a.popular && b.popular) return 1;
        if (a.year && b.year) return b.year - a.year;
        return a.name.localeCompare(b.name);
      }),
    }));
}
