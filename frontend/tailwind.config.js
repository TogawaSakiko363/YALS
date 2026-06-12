/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{js,ts,jsx,tsx}'],
  theme: {
    extend: {
      // Unify the utility-class corner radii (rounded, rounded-md, rounded-lg…)
      // onto the same --radius token the hand-written CSS uses, so JSX that
      // leans on Tailwind matches the rest of the app. full/none keep defaults.
      borderRadius: {
        sm: 'var(--radius)',
        DEFAULT: 'var(--radius)',
        md: 'var(--radius)',
        lg: 'var(--radius)',
        xl: 'var(--radius)',
        '2xl': 'var(--radius)',
        '3xl': 'var(--radius)',
      },
    },
  },
  plugins: [],
};
