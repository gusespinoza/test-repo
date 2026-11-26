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

export interface EventDetail extends Event {
  available: number;
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

export interface AdminEvent extends Event {
  sold: number;
}

export interface AdminEventPage {
  items: AdminEvent[];
  page: number;
  limit: number;
  total: number;
}
