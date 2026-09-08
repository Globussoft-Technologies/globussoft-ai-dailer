import { useContext } from 'react';
import { EventContext } from '../contexts/sseEventContext';

export function useCallifiedEvents() {
  return useContext(EventContext);
}
