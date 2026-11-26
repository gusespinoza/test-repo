export type BookingStatus = 'pending' | 'confirmed' | 'cancelled';

export interface Order {
  id: string;
  user_id: string;
  event_id: string;
  quantity: number;
  status: BookingStatus;
  idempotency_key: string;
  expires_at: string;
  created_at: string;
}

export interface AdminBooking extends Order {
  user_email: string;
  event_title: string;
}

export interface AdminBookingPage {
  items: AdminBooking[];
  page: number;
  limit: number;
  total: number;
}

export interface CreateBookingRequest {
  event_id: string;
  quantity: number;
  idempotency_key: string;
}
