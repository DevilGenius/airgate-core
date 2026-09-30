import { Card as HeroCard } from '@heroui/react';
import type { ComponentProps } from 'react';
import styles from './Card.module.css';

/** The application surface. Theme colors stay owned by the SDK. */
function CardRoot({ className, density = 'comfortable', ...props }: ComponentProps<typeof HeroCard> & {
  density?: 'comfortable' | 'compact';
}) {
  return <HeroCard {...props} data-density={density} className={[styles.card, className].filter(Boolean).join(' ')} />;
}

export const Card = Object.assign(CardRoot, {
  Content: HeroCard.Content,
  Header: HeroCard.Header,
  Title: HeroCard.Title,
  Description: HeroCard.Description,
  Footer: HeroCard.Footer,
});
