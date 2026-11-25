export interface Event {
  id: string;
  title: string;
  description: string;
  venue: string;
  starts_at: string;
  capacity: number;
  created_at: string;
  updated_at: string;
}

export interface EventInput {
  title: string;
  description: string;
  venue: string;
  starts_at: string;
  capacity: number;
}

export interface EventPage {
  items: Event[];
  page: number;
  limit: number;
  total: number;
}
